package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

const uploadLimit int64 = 1 << 30

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Can't update video as current user", nil)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}
	filePath := getAssetPath(mediaType)

	temp, err := os.CreateTemp("", "upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	_, err = io.Copy(temp, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file", err)
		return
	}
	aspectRatio, err := getVideoAspectRatio(temp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get aspect ratio", err)
		return
	}
	filePath = fmt.Sprintf("%s/%s", aspectRatio, filePath)
	temp.Seek(0, io.SeekStart)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &filePath,
		Body:        temp,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file to S3", err)
		return
	}
	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, filePath)
	video.VideoURL = &videoURL
	if err = cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video in database", err)
	}
}

func getVideoAspectRatio(filePath string) (string, error) {
	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	buffer := bytes.NewBuffer([]byte{})
	command.Stdout = buffer
	if err := command.Run(); err != nil {
		return "", err
	}
	result := struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}{}
	if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
		return "", err
	}
	width := result.Streams[0].Width
	height := result.Streams[0].Height

	const (
		sixteenByNine float64 = 16.0 / 9.0
		nineBySixteen float64 = 9.0 / 16.0
		tolerance     float64 = 0.01
	)
	aspectRatio := float64(width) / float64(height)
	if math.Abs(aspectRatio-sixteenByNine) < tolerance {
		return "landscape", nil
	} else if math.Abs(aspectRatio-nineBySixteen) < tolerance {
		return "portrait", nil
	} else {
		return "other", nil
	}
}

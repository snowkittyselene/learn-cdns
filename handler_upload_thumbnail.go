package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

const maxMemory int64 = 10 << 20

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't parse form", err)
		return
	}
	data, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse form file", err)
		return
	}
	contentType := header.Header.Get("Content-Type")

	imageData, err := io.ReadAll(data)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't decode image data", err)
		return
	}
	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video info", err)
		return
	}
	if userID != metadata.UserID {
		respondWithError(w, http.StatusUnauthorized, "Couldn't update video", err)
		return
	}

	imageDataString := base64.StdEncoding.EncodeToString(imageData)

	tnURL := fmt.Sprintf("data:%s;base64,%s", contentType, imageDataString)

	updatedVideo := database.Video{
		ID:                videoID,
		CreatedAt:         metadata.CreatedAt,
		UpdatedAt:         time.Now(),
		ThumbnailURL:      &tnURL,
		VideoURL:          metadata.VideoURL,
		CreateVideoParams: metadata.CreateVideoParams,
	}
	if err = cfg.db.UpdateVideo(updatedVideo); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updatedVideo)
}

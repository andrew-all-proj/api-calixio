package dto

type InitMediaUploadRequest struct {
	FileName    string `json:"fileName" validate:"required"`
	ContentType string `json:"contentType" validate:"required"`
	SizeBytes   int64  `json:"sizeBytes" validate:"required,gt=0"`
}

type InitMediaUploadResponse struct {
	MediaID    string `json:"mediaId"`
	UploadURL  string `json:"uploadUrl"`
	StorageKey string `json:"storageKey"`
}

type CompleteMediaUploadRequest struct {
	MediaID string `json:"mediaId" validate:"required"`
}

type CompleteMediaUploadResponse struct {
	MediaID string `json:"mediaId"`
	Status  string `json:"status"`
}

type MediaListItemResponse struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	OriginalName  string  `json:"originalName"`
	PlaybackURL   string  `json:"playbackUrl"`
	PreviewURL    *string `json:"previewUrl,omitempty"`
	DurationSec   *int    `json:"durationSec,omitempty"`
	FileSizeBytes int64   `json:"fileSizeBytes"`
	MimeType      string  `json:"mimeType"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"createdAt"`
}

type PlaybackMediaResponse struct {
	MediaID     string  `json:"mediaId"`
	Status      string  `json:"status"`
	Manifest    string  `json:"manifest"`
	ManifestURL *string `json:"manifestUrl,omitempty"`
	PreviewURL  *string `json:"previewUrl,omitempty"`
	ExpiresAt   string  `json:"expiresAt"`
}

type DeleteMediaResponse struct {
	MediaID string `json:"mediaId"`
	Status  string `json:"status"`
}

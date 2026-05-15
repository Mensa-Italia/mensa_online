package podcastsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const (
	playlistMetadataTimeout = 60 * time.Second
	episodeDownloadTimeout  = 30 * time.Minute
)

// PlaylistMetadata e` il subset dei campi del JSON di yt-dlp che ci serve
// per popolare la serie podcast.
type PlaylistMetadata struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Uploader    string             `json:"uploader"`
	Thumbnails  []PlaylistThumb    `json:"thumbnails"`
	Entries     []PlaylistEntry    `json:"entries"`
}

type PlaylistThumb struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PlaylistEntry e` un singolo video nella playlist, in modalita` flat
// (metadati minimi, niente download).
type PlaylistEntry struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Duration      float64 `json:"duration"`
	UploadDate    string  `json:"upload_date"` // YYYYMMDD
	Thumbnail     string  `json:"thumbnail"`
	Description   string  `json:"description"`
}

// FetchPlaylistFlat lista i video della playlist senza scaricarli. Veloce.
// L'output di yt-dlp con --flat-playlist ha solo id/title/duration per entry.
func FetchPlaylistFlat(playlistID string) (*PlaylistMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), playlistMetadataTimeout)
	defer cancel()

	url := "https://www.youtube.com/playlist?list=" + playlistID
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"-J",
		"--flat-playlist",
		"--no-warnings",
		url,
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp playlist: %w (stderr=%s)", err, stderr.String())
	}
	var meta PlaylistMetadata
	if err := json.Unmarshal(out.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("decode playlist JSON: %w", err)
	}
	return &meta, nil
}

// EpisodeDownload e` il risultato del download di un singolo video.
type EpisodeDownload struct {
	VideoID         string
	Title           string
	Description     string
	UploadDate      string // YYYYMMDD
	DurationSeconds int
	AudioPath       string // path locale al file mp3 scaricato
	ThumbnailPath   string // path locale alla thumbnail jpg/webp
	ThumbnailURL    string // URL diretto della thumbnail (se yt-dlp non l'ha scritta su disco)
}

// VideoInfo wraps yt-dlp's per-video --print-json output.
type VideoInfo struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	UploadDate  string  `json:"upload_date"`
	Duration    float64 `json:"duration"`
	Thumbnail   string  `json:"thumbnail"`
}

// DownloadEpisode scarica audio (mp3 best quality) + thumbnail per il video
// indicato in outDir. Ritorna i path locali e i metadati. Il chiamante e`
// responsabile di pulire outDir.
func DownloadEpisode(videoID, outDir string) (*EpisodeDownload, error) {
	ctx, cancel := context.WithTimeout(context.Background(), episodeDownloadTimeout)
	defer cancel()

	url := "https://www.youtube.com/watch?v=" + videoID
	outTpl := outDir + "/%(id)s.%(ext)s"
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"--write-thumbnail",
		"--convert-thumbnails", "jpg",
		"--no-warnings",
		"--print-json",
		"-o", outTpl,
		url,
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp download %s: %w (stderr=%s)", videoID, err, stderr.String())
	}

	var info VideoInfo
	// --print-json puo' emettere piu' linee se ci sono fasi (es. raw poi
	// convertito): tieni l'ultima JSON valida.
	for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var tmp VideoInfo
		if err := json.Unmarshal(line, &tmp); err == nil && tmp.ID != "" {
			info = tmp
		}
	}
	if info.ID == "" {
		return nil, fmt.Errorf("yt-dlp non ha emesso json per %s", videoID)
	}

	return &EpisodeDownload{
		VideoID:         info.ID,
		Title:           info.Title,
		Description:     info.Description,
		UploadDate:      info.UploadDate,
		DurationSeconds: int(info.Duration),
		AudioPath:       outDir + "/" + info.ID + ".mp3",
		ThumbnailPath:   outDir + "/" + info.ID + ".jpg",
		ThumbnailURL:    info.Thumbnail,
	}, nil
}

// ParseUploadDate trasforma "YYYYMMDD" di yt-dlp in time.Time.
func ParseUploadDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	return time.Parse("20060102", s)
}

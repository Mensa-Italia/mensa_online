package podcastsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// SubtitlePath: path locale al file .vtt con sottotitoli italiani
	// (manual subs > auto-generated). Stringa vuota se YouTube non ne
	// espone, fallback alla trascrizione locale.
	SubtitlePath string
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
		// Sottotitoli: scarica quelli manuali italiani se ci sono,
		// altrimenti quelli auto-generati. Sempre in formato VTT con
		// timestamp nativi.
		"--write-subs", "--write-auto-subs",
		"--sub-langs", "it,it-IT,it-orig,it.*",
		"--sub-format", "vtt",
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
		SubtitlePath:    pickSubtitlePath(outDir, info.ID),
	}, nil
}

// pickSubtitlePath cerca un file .vtt italiano salvato da yt-dlp.
// yt-dlp salva file con suffissi tipo `<id>.it.vtt`, `<id>.it-IT.vtt`,
// `<id>.it-orig.vtt` (sub originali) o `<id>.it.<lang>.vtt` (auto).
// Preferenza: manuali italiani > auto-generati italiani > qualsiasi italiano.
func pickSubtitlePath(outDir, videoID string) string {
	candidates := []string{
		outDir + "/" + videoID + ".it.vtt",
		outDir + "/" + videoID + ".it-IT.vtt",
		outDir + "/" + videoID + ".it-orig.vtt",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Glob fallback per varianti che non abbiamo previsto.
	matches, _ := filepath.Glob(outDir + "/" + videoID + ".it*.vtt")
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// ParseUploadDate trasforma "YYYYMMDD" di yt-dlp in time.Time.
func ParseUploadDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	return time.Parse("20060102", s)
}

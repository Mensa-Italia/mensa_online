package quidaudio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	// Gemini TTS restituisce PCM signed 16-bit little endian, 24kHz mono.
	pcmSampleRate    = 24000
	pcmChannels      = 1
	pcmBitsPerSample = 16

	mp3Bitrate    = "64k" // mono parlato: 64k e` indistinguibile da 128k
	ffmpegTimeout = 120 * time.Second
)

// EncodePCMToMP3 prende il PCM raw che esce da Gemini TTS e lo transcodifica
// in MP3 64kbps mono usando ffmpeg via subprocess. Aspettative su ffmpeg:
// installato nel container (vedi Dockerfile, alpine package `ffmpeg`).
func EncodePCMToMP3(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("encode: pcm vuoto")
	}

	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", pcmSampleRate),
		"-ac", fmt.Sprintf("%d", pcmChannels),
		"-i", "pipe:0",
		"-codec:a", "libmp3lame",
		"-b:a", mp3Bitrate,
		"-f", "mp3",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(pcm)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w (stderr=%s)", err, stderr.String())
	}
	return out.Bytes(), nil
}

// pcmDurationSeconds calcola la durata in secondi del PCM raw, usata per
// popolare il campo duration_seconds di quid_articles_audio.
func pcmDurationSeconds(pcm []byte) int {
	bytesPerSample := pcmBitsPerSample / 8
	samples := len(pcm) / (bytesPerSample * pcmChannels)
	if samples == 0 {
		return 0
	}
	return samples / pcmSampleRate
}

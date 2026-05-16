package podcastsync

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// VTTSegment e` un blocco temporizzato del file VTT, equivalente al
// Segment di podcasttranscribe ma definito qui per evitare import ciclico.
type VTTSegment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

// VTTResult e` quello che ritorna ParseVTT: testo piatto + segments puliti.
type VTTResult struct {
	Transcript      string       `json:"transcript"`
	Segments        []VTTSegment `json:"segments"`
	DurationSeconds int          `json:"duration_seconds"`
}

// vttTimingRE matcha le linee timing tipo "00:00:01.234 --> 00:00:05.678".
var vttTimingRE = regexp.MustCompile(`^(\d{2,}):(\d{2}):(\d{2})\.(\d{3})\s+-->\s+(\d{2,}):(\d{2}):(\d{2})\.(\d{3})`)

// vttTagRE rimuove i tag HTML/karaoke che YouTube inserisce nei sub
// auto-generati (es. <c>, <00:00:01.234>, &nbsp;, ecc.).
var vttTagRE = regexp.MustCompile(`<[^>]+>`)

// ParseVTT legge un file VTT WebVTT (standard YouTube) e ne estrae
// transcript piatto + segments. Gestisce il caso "auto-generated YouTube":
// righe duplicate cue con effetto karaoke, parole inline-timestamped.
// Restituisce VTTResult con segments collassati e testo deduplicato.
func ParseVTT(path string) (*VTTResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open vtt: %w", err)
	}
	defer func() { _ = f.Close() }()

	var (
		segments []VTTSegment
		curStart float64
		curEnd   float64
		curText  strings.Builder
		inCue    bool
		seenText = map[string]struct{}{}
	)

	flush := func() {
		if !inCue {
			return
		}
		text := cleanText(curText.String())
		if text != "" {
			// YouTube auto-subs duplica ogni riga (rolling cue): se l'ultimo
			// testo finisce uguale al nuovo, lo evitiamo. Usiamo una hash-set
			// in piu` per skippare le ripetizioni a finestra > 1.
			if _, dup := seenText[text]; !dup {
				seenText[text] = struct{}{}
				segments = append(segments, VTTSegment{
					StartSeconds: curStart,
					EndSeconds:   curEnd,
					Text:         text,
				})
			}
		}
		curText.Reset()
		inCue = false
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24) // line buffer fino a 16MB per cue lunghi
	for scanner.Scan() {
		line := scanner.Text()
		if m := vttTimingRE.FindStringSubmatch(line); m != nil {
			flush()
			curStart = vttTimeToSeconds(m[1], m[2], m[3], m[4])
			curEnd = vttTimeToSeconds(m[5], m[6], m[7], m[8])
			inCue = true
			continue
		}
		if !inCue {
			continue
		}
		if line == "" {
			flush()
			continue
		}
		if curText.Len() > 0 {
			curText.WriteByte(' ')
		}
		curText.WriteString(line)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan vtt: %w", err)
	}

	if len(segments) == 0 {
		return &VTTResult{}, nil
	}

	// Build transcript piatto.
	var transcript strings.Builder
	for i, s := range segments {
		if i > 0 {
			transcript.WriteByte(' ')
		}
		transcript.WriteString(s.Text)
	}

	dur := int(segments[len(segments)-1].EndSeconds)
	return &VTTResult{
		Transcript:      transcript.String(),
		Segments:        segments,
		DurationSeconds: dur,
	}, nil
}

func vttTimeToSeconds(h, m, s, ms string) float64 {
	hi, _ := strconv.Atoi(h)
	mi, _ := strconv.Atoi(m)
	si, _ := strconv.Atoi(s)
	msi, _ := strconv.Atoi(ms)
	return float64(hi*3600+mi*60+si) + float64(msi)/1000.0
}

// shiftSegments scala tutti i timestamp di -offsetSeconds e scarta i
// segments che cadono interamente prima dello start (es. se la VTT contiene
// un intro che e` stato tagliato dal silenzio). I segments che cadono
// parzialmente prima vengono troncati a 0.
func shiftSegments(segs []VTTSegment, offsetSeconds float64) []VTTSegment {
	if offsetSeconds <= 0 || len(segs) == 0 {
		return segs
	}
	out := make([]VTTSegment, 0, len(segs))
	for _, s := range segs {
		newStart := s.StartSeconds - offsetSeconds
		newEnd := s.EndSeconds - offsetSeconds
		if newEnd <= 0 {
			continue // segmento intero pre-offset (musica intro, intro silenzio)
		}
		if newStart < 0 {
			newStart = 0
		}
		out = append(out, VTTSegment{
			StartSeconds: newStart,
			EndSeconds:   newEnd,
			Text:         s.Text,
		})
	}
	return out
}

func cleanText(s string) string {
	s = vttTagRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	// Riduci whitespace ridondante.
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

package player

import (
	"bytes"
	"io"
	"testing"
)

func TestParseStreamTitle(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantFound bool
	}{
		{"StreamTitle='RUDE - Eternal Youth';StreamUrl='';", "RUDE - Eternal Youth", true},
		{"StreamTitle='Bach: Cello Suite No.1';", "Bach: Cello Suite No.1", true},
		{"StreamTitle='Handel - Suite HWV436 {+info: veniceclassicradio.eu}';", "Handel - Suite HWV436", true},
		{"StreamUrl='http://x';", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		block := append([]byte(c.in), 0, 0, 0) // null padding, as servers send
		got, ok := parseStreamTitle(block)
		if ok != c.wantFound || got != c.want {
			t.Errorf("parseStreamTitle(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.wantFound)
		}
	}
}

func TestSplitStreamTitle(t *testing.T) {
	a, tr := splitStreamTitle("RUDE - Eternal Youth")
	if a != "RUDE" || tr != "Eternal Youth" {
		t.Errorf("got %q/%q", a, tr)
	}
	a, tr = splitStreamTitle("Just A Title")
	if a != "" || tr != "Just A Title" {
		t.Errorf("got %q/%q", a, tr)
	}
}

// buildICYStream frames audio with an ICY metadata block every metaint bytes.
func buildICYStream(metaint int, audio []byte, title string) []byte {
	var out bytes.Buffer
	meta := []byte("StreamTitle='" + title + "';")
	blocks := (len(meta) + 15) / 16
	padded := make([]byte, blocks*16)
	copy(padded, meta)

	for off := 0; off < len(audio); off += metaint {
		end := off + metaint
		if end > len(audio) {
			end = len(audio)
		}
		out.Write(audio[off:end])
		out.WriteByte(byte(blocks)) // length byte (in 16-byte units)
		out.Write(padded)
	}
	return out.Bytes()
}

func TestICYReaderDeinterleaves(t *testing.T) {
	metaint := 64
	audio := bytes.Repeat([]byte("STOA"), 96) // 384 bytes = 6 full metaint intervals
	raw := buildICYStream(metaint, audio, "RUDE - Eternal Youth")

	var got []string
	r := newICYReader(io.NopCloser(bytes.NewReader(raw)), metaint, func(title string) {
		got = append(got, title)
	})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(out, audio) {
		t.Fatalf("de-interleaved audio mismatch: got %d bytes, want %d", len(out), len(audio))
	}
	if len(got) == 0 || got[0] != "RUDE - Eternal Youth" {
		t.Fatalf("titles = %v; want first = RUDE - Eternal Youth", got)
	}
	// Repeat titles are suppressed: only one callback despite many blocks.
	if len(got) != 1 {
		t.Fatalf("expected 1 (deduped) callback, got %d", len(got))
	}
}

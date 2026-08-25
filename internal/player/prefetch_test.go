package player

import (
	"bytes"
	"io"
	"testing"
	"time"
)

// slowReader delivers src in small chunks with a tiny delay, imitating a
// network stream that dribbles data (and stalls) rather than returning it all
// at once.
type slowReader struct {
	data []byte
	pos  int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	time.Sleep(time.Millisecond)
	n := copy(p, r.data[r.pos:min(r.pos+512, len(r.data))])
	r.pos += n
	return n, nil
}

func (r *slowReader) Close() error { return nil }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestPrefetchReaderIntegrity checks that everything written by the source is
// read back byte-for-byte, in order, through the background-filled buffer.
func TestPrefetchReaderIntegrity(t *testing.T) {
	want := bytes.Repeat([]byte("stoa-radio-"), 4096) // ~45 KB
	pr := newPrefetchReader(&slowReader{data: want}, 8<<10)
	defer pr.Close()

	pr.waitReady(4<<10, time.Second)

	got, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("data mismatch: got %d bytes, want %d", len(got), len(want))
	}
}

// TestPrefetchReaderCloseUnblocks ensures Close wakes a reader that is blocked
// waiting for data on a source that never delivers.
func TestPrefetchReaderCloseUnblocks(t *testing.T) {
	pr := newPrefetchReader(io.NopCloser(&blockingReader{}), 8<<10)
	done := make(chan struct{})
	go func() {
		pr.Read(make([]byte, 16))
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	pr.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Read did not unblock after Close")
	}
}

// blockingReader never returns, modelling a fully stalled connection.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }

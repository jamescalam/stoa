//go:build !darwin || !cgo

package media

// Service is a no-op on platforms without the macOS MediaPlayer bridge.
type Service struct{}

// New returns an inert service.
func New(Commands) *Service { return &Service{} }

// Update does nothing.
func (s *Service) Update(NowPlaying) {}

// Run simply runs the UI on the current goroutine.
func (s *Service) Run(tui func() error) error { return tui() }

// Close does nothing.
func (s *Service) Close() {}

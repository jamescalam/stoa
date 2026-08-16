//go:build !darwin || !cgo

package media

// Service is a no-op on platforms without the macOS MediaPlayer bridge.
type Service struct{}

// New returns an inert service.
func New(Commands) *Service { return &Service{} }

// Update does nothing.
func (s *Service) Update(NowPlaying) {}

// CurrentDevice returns no device on platforms without CoreAudio.
func (s *Service) CurrentDevice() Device { return Device{} }

// Run simply runs the UI on the current goroutine.
func (s *Service) Run(tui func() error) error { return tui() }

// Close does nothing.
func (s *Service) Close() {}

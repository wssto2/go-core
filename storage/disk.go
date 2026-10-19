package storage

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wssto2/go-core/storage/local"
	"github.com/wssto2/go-core/storage/memory"
)

var (
	errBaseURL    = errors.New("the public base URL is neither a path nor an http(s) URL")
	errTwoDrivers = errors.New("it names more than one driver")
	errName       = errors.New("a disk has an empty name, or one with surrounding spaces")
	errTwice      = errors.New("is declared twice")
	errNoDriver   = errors.New("says where its files live neither with Local, Memory nor Driver")
	errUnknown    = errors.New("no disk is declared")
)

// DiskSpec describes a named disk; build one with Disk and register it with the application's Disks.
type DiskSpec struct {
	name   string
	driver Driver
	public bool
	base   string
	err    error
}

// Disk starts the description of the disk called name. Say where its files live (Local, Memory or Driver); a disk is
// private unless it also says Public.
//
//	storage.Disk("uploads").Local("storage/uploads")                 // served only through the app
//	storage.Disk("public").Local("storage/public").Public("/media")  // anyone with the URL can open it
func Disk(name string) DiskSpec {
	return DiskSpec{name: name, driver: nil, public: false, base: "", err: nil}
}

// Name is the disk's name.
func (s DiskSpec) Name() string { return s.name }

// Local keeps the files under root on the local file system. The root is created when missing.
func (s DiskSpec) Local(root string) DiskSpec {
	d, err := local.New(root)
	if err != nil {
		return s.with(nil, fmt.Errorf("its root %q cannot be used: %w", root, err))
	}

	return s.with(d, nil)
}

// Memory keeps the files in memory, for tests.
func (s DiskSpec) Memory() DiskSpec {
	d, err := memory.New()

	return s.with(d, err)
}

// Driver keeps the files in d, for a store go-core does not ship.
func (s DiskSpec) Driver(d Driver) DiskSpec { return s.with(d, nil) }

// Public makes the disk's files reachable at baseURL, a path ("/media") or an absolute URL
// ("https://cdn.example.com/media"): anyone with the link can open one. Without it the disk is private and URL is an
// error.
func (s DiskSpec) Public(baseURL string) DiskSpec {
	s.public = true
	s.base = strings.TrimRight(baseURL, "/")

	absolute := func(scheme string) bool { return strings.HasPrefix(baseURL, scheme) }
	if !slices.ContainsFunc([]string{"/", "http://", "https://"}, absolute) {
		s.err = errors.Join(s.err, fmt.Errorf("%w: %q", errBaseURL, baseURL))
	}

	return s
}

func (s DiskSpec) with(driver Driver, err error) DiskSpec {
	if s.driver != nil {
		err = errors.Join(err, errTwoDrivers)
	}

	if err == nil {
		s.driver = driver
	}

	s.err = errors.Join(s.err, err)

	return s
}

// Registry holds the disks of an application by name.
type Registry struct {
	disks map[string]*Files
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{disks: map[string]*Files{}} }

// Add registers the disk, or says why it cannot be: no name, a name used twice, no driver, a root that cannot be used.
func (r *Registry) Add(spec DiskSpec) error {
	switch {
	case spec.name == "" || strings.TrimSpace(spec.name) != spec.name:
		return errName
	case r.disks[spec.name] != nil:
		return fmt.Errorf("disk %q %w", spec.name, errTwice)
	case spec.err != nil:
		return fmt.Errorf("disk %q: %w", spec.name, spec.err)
	case spec.driver == nil:
		return fmt.Errorf("disk %q %w", spec.name, errNoDriver)
	}

	base := ""
	if spec.public {
		base = spec.base
	}

	r.disks[spec.name] = &Files{
		driver: spec.driver, disk: spec.name, prefix: "", base: base, public: spec.public, err: nil,
	}

	return nil
}

// Disk returns the registered disk.
func (r *Registry) Disk(name string) (*Files, bool) {
	f, ok := r.disks[name]

	return f, ok
}

// Host is what Use needs of the application: gocore.App is one.
type Host interface {
	// Disk returns the declared disk.
	Disk(name string) (*Files, bool)
	// Fail reports a problem that stops the application from starting.
	Fail(what, fix string)
}

// Use returns the disk declared as name. An unknown name stops the application at start-up, not at first use: the
// problem is reported with the others by Run and Check, and the returned disk answers every call with an error.
// Declare the disks (Disks) before the features that use them.
//
//	documents := storage.Use(app, "uploads").Folder("documents")
func Use(h Host, name string) *Files {
	if f, ok := h.Disk(name); ok {
		return f
	}

	h.Fail(fmt.Sprintf("no disk is declared as %q", name),
		fmt.Sprintf("declare it with app.Disks(storage.Disk(%q).Local(root)) before the feature that uses it", name))

	return &Files{
		driver: nil, disk: name, prefix: "", base: "", public: false,
		err: fmt.Errorf("storage: %w as %q", errUnknown, name),
	}
}

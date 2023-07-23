package fprintf

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

var (
	fileHandlers map[string]FileHandler = make(map[string]FileHandler)
)

// FileHandler returns an io.WriteCloser that will handle the details of writing to the given
// filename. The filename could respresent a resource, such as a database, or a file.
type FileHandler func(ctx context.Context, filename string) (io.WriteCloser, error)

func init() {
	fileHandlers["file"] = newFileHandler
}

type fileHandler struct {
	name     string
	tempTemp string
	f        io.WriteCloser
}

func newFileHandler(ctx context.Context, filename string) (io.WriteCloser, error) {

	dirname := path.Dir(filename)
	isTemp := false
	if strings.HasPrefix(filename, "/$tmp/") {
		dirname = ""
		filename = strings.TrimPrefix(filename, "/$tmp/")
		isTemp = true
	}

	baseFile := path.Base(filename)
	f, err := os.CreateTemp(dirname, baseFile)
	if err != nil {
		return nil, err
	}
	if isTemp {
		filename = path.Join(os.TempDir(), baseFile)
	}

	fh := &fileHandler{
		name:     filename,
		f:        f,
		tempTemp: f.Name(),
	}
	go fh.watchTilClose(ctx)
	return fh, nil
}

func (fh *fileHandler) watchTilClose(ctx context.Context) {
	<-ctx.Done()
	fh.f.Close()
	os.Remove(fh.tempTemp)
}

func (fh *fileHandler) Write(p []byte) (n int, err error) {
	return fh.f.Write(p)
}

func (fh *fileHandler) Close() error {
	if err := fh.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(fh.tempTemp, fh.name); err != nil {
		return err
	}
	return nil
}

// RegisterFileHandler a new file handler. This will panic if the name is already registered.
func RegisterFileHandler(name string, f FileHandler) {
	if _, ok := fileHandlers[name]; ok {
		panic(fmt.Sprintf("fprintf: %s already registered", name))
	}
	fileHandlers[name] = f
}

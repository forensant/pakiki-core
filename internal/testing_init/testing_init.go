package testing_init

// from: https://intellij-support.jetbrains.com/hc/en-us/community/posts/360009685279-Go-test-working-directory-keeps-changing-to-dir-of-the-test-file-instead-of-value-in-template

import (
	"os"
	"path"
	"runtime"

	"github.com/pipeline/proximity-core/internal/request_queue"
	"github.com/pipeline/proximity-core/pkg/project"
	"github.com/zalando/go-keyring"
)

func init() {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "../../build")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	keyring.MockInit()
	request_queue.Init()

	pf, _ := os.CreateTemp("", "proximity-testprojdb-*")
	tf, _ := os.CreateTemp("", "proximity-testtmpdb-*")
	ioHub := project.NewIOHub()
	ioHub.Run(pf.Name(), tf.Name())
}

package testing_init

// from: https://intellij-support.jetbrains.com/hc/en-us/community/posts/360009685279-Go-test-working-directory-keeps-changing-to-dir-of-the-test-file-instead-of-value-in-template

import (
	"os"
	"path"
	"runtime"

	"github.com/forensant/pakiki-core/internal/request_queue"
	"github.com/forensant/pakiki-core/pkg/project"
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

	pf, _ := os.CreateTemp("", "pakiki-testprojdb-*")
	tf, _ := os.CreateTemp("", "pakiki-testtmpdb-*")
	ioHub := project.NewIOHub("", "")
	ioHub.Run(pf.Name(), tf.Name())
}

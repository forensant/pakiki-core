#!/usr/bin/env bash

# from: https://medium.com/@taowen/go-test-coverage-for-multi-package-project-d4f36f2b573a

# this assumes it's being run from the build directory

set -e
echo "" > coverage.txt

ln -s $PWD/proximitypythoninterpreter ../internal/scripting/
ln -s $PWD/python310 ../internal/scripting/

for d in $(go list ../... | grep -v vendor); do
    if [[ "$OSTYPE" == "darwin"* ]]; then    
        CGO_CFLAGS=-Wno-undef-prefix CGO_ENABLED=1 GOOS=darwin go test -coverprofile=profile.out -coverpkg=github.com/pipeline/proximity-core,github.com/pipeline/proximity-core/pkg/project,github.com/pipeline/proximity-core/internal/proxy,github.com/pipeline/proximity-core/internal/request_queue,github.com/pipeline/proximity-core/internal/scripting $d
    else
        go test -coverprofile=profile.out -coverpkg=github.com/pipeline/proximity-core,github.com/pipeline/proximity-core/pkg/project,github.com/pipeline/proximity-core/internal/proxy,github.com/pipeline/proximity-core/internal/request_queue,github.com/pipeline/proximity-core/internal/scripting $d
    fi
    
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done

rm ../internal/scripting/proximitypythoninterpreter
rm ../internal/scripting/python310
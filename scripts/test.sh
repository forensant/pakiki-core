#!/usr/bin/env bash

# from: https://medium.com/@taowen/go-test-coverage-for-multi-package-project-d4f36f2b573a

set -e
echo "" > coverage.txt

for d in $(go list ../... | grep -v vendor); do
    go test -coverprofile=profile.out -coverpkg=dev.forensant.com/pipeline/razor/proximitycore,dev.forensant.com/pipeline/razor/proximitycore/ca,dev.forensant.com/pipeline/razor/proximitycore/docs,dev.forensant.com/pipeline/razor/proximitycore/project,dev.forensant.com/pipeline/razor/proximitycore/proxy,dev.forensant.com/pipeline/razor/proximitycore/proxy/interactsh,dev.forensant.com/pipeline/razor/proximitycore/proxy/request_queue,dev.forensant.com/pipeline/razor/proximitycore/scripting $d
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done

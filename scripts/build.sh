#!/bin/bash

# generate dependencies and tidy the project
echo "# Generating Swagger documents"
swag init -o api -g cmd/pakikicore/main.go --parseInternal

#go mod tidy

# build cyberchef
export NODE_OPTIONS=--max_old_space_size=2048
cd www/cyberchef
npm install
npm run build

cd ../..

commit_sha=$(git rev-parse HEAD)

# now do the actual build
if [[ "$OSTYPE" == "darwin"* ]]; then
    # MacOS is handled separately, so that it can be compiled for both arm64 and amd64 architectures
    mkdir -p build/python310_arm64/lib
    mkdir -p build/python310_x64/lib

    echo "# Building Python interpreter"
    gcc -std=c++17 $(arch --x86_64 /usr/local/bin/python3.10-config --cflags) $(arch --x86_64 /usr/local/bin/python3.10-config --ldflags) $(arch --x86_64 /usr/local/bin/python3.10-config --libs) -lpython3.10 -lstdc++ -std=c++17 tools/PythonInterpreter.cpp -target x86_64-apple-macos10.15 -o build/PythonInterpreter_x86_64
    ln -s $(arch --x86_64 /usr/local/bin/python3.10 -c "import sys; print(sys.base_prefix + '/lib/python3.10/')") build/python310_x64/lib/

    # For ARM compilation for Python, uncomment the following lines, and comment out the corresponding ones above
    MACOSX_DEPLOYMENT_TARGET=10.15 gcc -std=c++17 -mmacosx-version-min=10.15 $(python3.10-config --cflags) $(python3.10-config --ldflags) $(python3.10-config --libs) -lpython3.10 -lstdc++ tools/PythonInterpreter.cpp -o build/PythonInterpreter_arm64
    ln -s $(python3.10 -c "import sys; print(sys.base_prefix + '/lib/python3.10/')") build/python310_arm64/lib/

    # For compiling both architectures into one executable (this is not currently done, as we'd need to bundle two sets of dependencies): 
    lipo -create -output build/pakikipythoninterpreter build/PythonInterpreter_arm64 build/PythonInterpreter_x86_64

    rm -rf build/PythonInterpreter_*

    echo "# Building Pākiki Core"
    CGO_CFLAGS=-Wno-undef-prefix CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.release=$commit_sha" -o build/PakikiCore_x86_64 cmd/pakikicore/main.go
    CGO_CFLAGS=-Wno-undef-prefix CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w -X main.release=$commit_sha" -o build/PakikiCore_arm64 cmd/pakikicore/main.go
    lipo -create -output build/pakikicore build/PakikiCore_x86_64 build/PakikiCore_arm64
    rm build/PakikiCore_*
else
    mkdir -p build/python313/lib

    # written on Linux, but would likely be similar for other Unix systems
    echo "# Building Python interpreter"
    gcc $(python3.13-config --cflags) $(python3.13-config --ldflags) $(python3.13-config --libs) -std=c++17 -fPIC tools/PythonInterpreter.cpp -o build/pakikipythoninterpreter -lstdc++ -lpython3.13
    
    cp -r $(python3.13-config --prefix)/lib/python3.13 build/python313/lib

    scripts/copy_lib_linux.rb build/

    echo "# Building Pākiki Core"
    go build -ldflags "-s -w -X main.release=$commit_sha" -o build/pakikicore cmd/pakikicore/main.go
fi

echo ""
echo ""
echo "Pākiki built :)"
echo "Run ./pakikicore from the build directory to get started"

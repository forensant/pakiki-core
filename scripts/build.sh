#!/bin/bash

# generate dependencies and tidy the project
echo "# Generating Swagger documents"
swag init -o api -g cmd/proximitycore/main.go --parseInternal

#go mod tidy

# now do the actual build
if [[ "$OSTYPE" == "darwin"* ]]; then
    # MacOS is handled separately, so that it can be compiled for both arm64 and amd64 architectures
    mkdir -p build/python310/lib

    echo "# Building Python interpreter"
    gcc $(arch --x86_64 /usr/local/bin/python3.10-config --cflags) $(arch --x86_64 /usr/local/bin/python3.10-config --ldflags) $(arch --x86_64 /usr/local/bin/python3.10-config --libs) -lpython3.10 -lstdc++ tools/PythonInterpreter.cpp -target x86_64-apple-macos10.12 -o build/PythonInterpreter_x86_64
    cp build/PythonInterpreter_x86_64 build/proximitypythoninterpreter
    ln -s $(arch --x86_64 /usr/local/bin/python3.10 -c "import sys; print(sys.base_prefix + '/lib/python3.10/')") build/python310/lib/

    # For ARM compilation for Python, uncomment the following lines, and comment out the corresponding ones above
    #gcc $(python3.10-config --cflags) $(python3.10-config --ldflags) $(python3.10-config --libs) -lpython3.10 -lstdc++ tools/PythonInterpreter.cpp -o build/PythonInterpreter_arm64
    #ln -s $(python3.10 -c "import sys; print(sys.base_prefix + '/lib/python3.10/')") build/python310/lib/
    #cp build/PythonInterpreter_arm64 build/pythoninterpreter

    # For compiling both architectures into one executable (this is not currently done, as we'd need to bundle two sets of dependencies): 
    #lipo -create -output build/pythoninterpreter build/PythonInterpreter_arm64 build/PythonInterpreter_x86_64

    rm -rf build/PythonInterpreter_*

    echo "# Building Proximity Core"
    CGO_CFLAGS=-Wno-undef-prefix CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o build/ProximityCore_x86_64 cmd/proximitycore/main.go
    CGO_CFLAGS=-Wno-undef-prefix CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o build/ProximityCore_arm64 cmd/proximitycore/main.go
    lipo -create -output build/proximitycore build/ProximityCore_x86_64 build/ProximityCore_arm64
    rm build/ProximityCore_*
else
    mkdir -p build/python310/lib

    # written on Linux, but would likely be similar for other Unix systems
    echo "# Building Python interpreter"
    gcc $(python3.10-config --cflags) $(python3.10-config --ldflags) $(python3.10-config --libs) -std=c++17 -fPIC tools/PythonInterpreter.cpp -o build/proximitypythoninterpreter -lstdc++ -lpython3.10
    
    cp -r $(python3.10-config --prefix)/lib/python3.10 build/python310/lib

    scripts/copy_lib_linux.rb build/

    echo "# Building Proximity Core"
    go build -ldflags "-s -w" -o build/proximitycore cmd/proximitycore/main.go
fi

echo ""
echo ""
echo "Proximity built :)"
echo "Run ./proximitycore from the build directory to get started"

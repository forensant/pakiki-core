#!/bin/bash

# generate dependencies and tidy the project
echo "# Generating Swagger documents"
swag init
go mod tidy

mkdir -p build/python39/lib

# now do the actual build
if [[ "$OSTYPE" == "darwin"* ]]; then
    # MacOS is handled separately, so that it can be compiled for both arm64 and amd64 architectures

    echo "# Building Python interpreter"
    gcc $(python3.9-config --cflags) $(python3.9-config --ldflags) $(python3.9-config --libs) -lpython3.9 -lstdc++ scripting/interpreter/PythonInterpreter.cpp -o build/PythonInterpreter_arm64
    gcc $(arch --x86_64 /usr/local/bin/python3.9-config --cflags) $(arch --x86_64 /usr/local/bin/python3.9-config --ldflags) $(arch --x86_64 /usr/local/bin/python3.9-config --libs) -lpython3.9 -lstdc++ scripting/interpreter/PythonInterpreter.cpp -target x86_64-apple-macos10.12 -o build/PythonInterpreter_x86_64
    lipo -create -output build/pythoninterpreter build/PythonInterpreter_arm64 build/PythonInterpreter_x86_64
    rm -rf build/PythonInterpreter_*
    ln -s /opt/homebrew/opt/python@3.9/Frameworks/Python.framework/Versions/3.9/lib/python3.9/ build/python39/lib/

    echo "# Building Proximity Core"
    CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o build/ProximityCore_amd64
    CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o build/ProximityCore_arm64
    lipo -create -output build/proximitycore build/ProximityCore_amd64 build/ProximityCore_arm64
    rm build/ProximityCore_*
else
    # written on Linux, but would likely be similar for other Unix systems
    echo "# Building Python interpreter"
    gcc $(python3.9-config --cflags) $(python3.9-config --ldflags) $(python3.9-config --libs) -fPIC scripting/interpreter/PythonInterpreter.cpp -o build/pythoninterpreter -lstdc++ -lpython3.9
    ln -s /usr/lib/python3.9/ build/python39/lib

    echo "# Building Proximity Core"
    go build -o build/proximitycore
fi

echo ""
echo ""
echo "Proximity built :)"
echo "Run ./proximitycore from the build directory to get started"

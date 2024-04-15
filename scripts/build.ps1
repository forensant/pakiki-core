# generate dependencies and tidy the project
Write-Output "# Generating Swagger documents"
swag init -o api -g cmd/pakikicore/main.go --parseInternal

# download cyberchef (builds aren't supported on Windows, so we just grab the pre-compiled version)
Write-Output "Downloading CyberChef"
$github_owner = "gchq"  # Replace with the repo owner
$github_repo = "cyberchef"      # Replace with the repo name
$download_folder = "www/cyberchef/build/prod/"

if(-not(Test-path "$download_folder/CyberChef.html" -PathType leaf)) {
    # Get the latest release URL
    $release_url = Invoke-RestMethod -Uri "https://api.github.com/repos/$github_owner/$github_repo/releases/latest" | Select-Object -ExpandProperty assets | Where-Object {$_.name -like "*.zip"} | Select-Object -ExpandProperty browser_download_url

    # Download the release ZIP
    Write-Host "Downloading latest release..."
    Invoke-WebRequest -Uri $release_url -OutFile "$download_folder\$github_repo.zip"

    # Extract the ZIP
    Write-Host "Extracting archive..."
    Expand-Archive -Path "$download_folder\$github_repo.zip" -DestinationPath $download_folder -Force

    # Rename Cyberchef HTML files
    Write-Host "Renaming Cyberchef HTML files..."
    Get-ChildItem -Path "$download_folder\CyberChef*.html" | ForEach-Object { Rename-Item -Path $_.FullName -NewName "CyberChef.html" }

    # Cleanup (optional - removes downloaded ZIP)
    Remove-Item -Path "$download_folder\$github_repo.zip"

    Write-Host "Download, extraction, and renaming complete!"
} else {
    Write-Output "CyberChef exists, skipping the download"
}

New-Item -ItemType Directory -Force -Path "build/python311/lib"

# Set-Location .

$commit_sha = git rev-parse HEAD

# Create the build directory structure
New-Item -ItemType Directory -Force -Path "build/python311/lib"

# Build the Python interpreter
#Write-Host "## Building Python interpreter"
#g++.exe (python3.11-config --cflags) (python3.11-config --ldflags) (python3.11-config --libs) -std:c++17 -fPIC tools/PythonInterpreter.cpp -o build/pakikipythoninterpreter -lstdc++ -lpython311

# Copy Python libraries
#Copy-Item -Recurse (python3.11-config --prefix)/lib/python311 build/python311/lib

# Build Pākiki Core
Write-Host "## Building Pākiki Core"
$ENV:CGO_ENABLED = "1"
go build -ldflags "-s -w -X main.release=$commit_sha" -o build/pakikicore.exe cmd/pakikicore/main.go

Write-Output ""
Write-Output ""
Write-Output "Pākiki built :)"
Write-Output "Run ./pakikicore.exe from the build directory to get started"

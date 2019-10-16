set GO111MODULE=on

set GOOS=windows
set GOARCH=amd64
go build -o d2d-win-amd64.exe -ldflags="-s -w" ../d2d/main.go
go build -o l2d-win-amd64.exe -ldflags="-s -w" ../l2d/main.go
go build -o l2l-win-amd64.exe -ldflags="-s -w" ../l2l/main.go

set GOOS=linux
set GOARCH=amd64
go build -o d2d-linux-amd64 -ldflags="-s -w" ../d2d/main.go
go build -o l2d-linux-amd64 -ldflags="-s -w" ../l2d/main.go
go build -o l2l-linux-amd64 -ldflags="-s -w" ../l2l/main.go

rem set GOARCH=386
rem go build -o d2d-linux-386 -ldflags="-s -w" ../d2d/main.go
rem go build -o l2d-linux-386 -ldflags="-s -w" ../l2d/main.go
rem go build -o l2l-linux-386 -ldflags="-s -w" ../l2l/main.go
rem set GOARCH=arm
rem go build -o d2d-linux-arm -ldflags="-s -w" ../d2d/main.go
rem go build -o l2d-linux-arm -ldflags="-s -w" ../l2d/main.go
rem go build -o l2l-linux-arm -ldflags="-s -w" ../l2l/main.go
rem set GOARCH=arm64
rem go build -o d2d-linux-arm64 -ldflags="-s -w" ../d2d/main.go
rem go build -o l2d-linux-arm64 -ldflags="-s -w" ../l2d/main.go
rem go build -o l2l-linux-arm64 -ldflags="-s -w" ../l2l/main.go
rem set GOARCH=mips
rem go build -o d2d-linux-mips -ldflags="-s -w" ../d2d/main.go
rem go build -o l2d-linux-mips -ldflags="-s -w" ../l2d/main.go
rem go build -o l2l-linux-mips -ldflags="-s -w" ../l2l/main.go
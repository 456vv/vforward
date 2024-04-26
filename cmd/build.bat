go mod tidy
go get -u

set GOOS=windows
set GOARCH=amd64
go build -o bin/d2d-win-amd64.exe -trimpath -ldflags="-s -w" d2d/main.go
go build -o bin/l2d-win-amd64.exe -trimpath -ldflags="-s -w" l2d/main.go
go build -o bin/l2l-win-amd64.exe -trimpath -ldflags="-s -w" l2l/main.go
go clean -cache

set GOOS=linux
set GOARCH=amd64
go build -o bin/d2d-linux-amd64 -trimpath -ldflags="-s -w" d2d/main.go
go build -o bin/l2d-linux-amd64 -trimpath -ldflags="-s -w" l2d/main.go
go build -o bin/l2l-linux-amd64 -trimpath -ldflags="-s -w" l2l/main.go
set GOARCH=arm
set GOARM=7
go build -o bin/d2d-linux-armv7 -trimpath -ldflags="-s -w" d2d/main.go
go build -o bin/l2d-linux-armv7 -trimpath -ldflags="-s -w" l2d/main.go
go build -o bin/l2l-linux-armv7 -trimpath -ldflags="-s -w" l2l/main.go
set GOARCH=arm64
go build -o bin/d2d-linux-arm64 -trimpath -ldflags="-s -w" d2d/main.go
go build -o bin/l2d-linux-arm64 -trimpath -ldflags="-s -w" l2d/main.go
go build -o bin/l2l-linux-arm64 -trimpath -ldflags="-s -w" l2l/main.go
set GOARCH=mips
go build -o bin/d2d-linux-mips -trimpath -ldflags="-s -w" d2d/main.go
go build -o bin/l2d-linux-mips -trimpath -ldflags="-s -w" l2d/main.go
go build -o bin/l2l-linux-mips -trimpath -ldflags="-s -w" l2l/main.go
go clean -cache

upx -9 bin/*
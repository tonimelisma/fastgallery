all: deps test build

deps:
	go get ./...

test:
	go test -v ./...

build:
	go build -o bin/fastgallery cmd/fastgallery/main.go

clean:
	rm bin/fastgallery
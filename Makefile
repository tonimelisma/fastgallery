ifndef GO
GO := go
endif

all: deps test build

deps:
	$(GO) get ./...

test:
	$(GO) test -v ./...

build:
	$(GO) build -o bin/fastgallery cmd/fastgallery/main.go

clean:
	rm bin/fastgallery

install:
	cp bin/fastgallery ~/.local/bin
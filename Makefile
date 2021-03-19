ifndef GO
GO := go
endif

all: deps test build

deps:
	$(GO) get ./...

build:
	$(GO) build -o bin/fastgallery cmd/fastgallery/main.go

test:
	$(GO) test -v ./...

testgallery: build
	rm -rf testing/gallery/
	rm -f /tmp/fastgallery.log
	bin/fastgallery --log /tmp/fastgallery.log --cleanup testing/source/ testing/gallery/

clean:
	rm bin/fastgallery

install: build
	cp bin/fastgallery ~/.local/bin
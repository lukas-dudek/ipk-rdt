# IPK Projekt 2 - Reliable UDP
# Student: Lukas Dudek

BINARY_NAME=ipk-rdt
SRC=main.go sender.go receiver.go protocol.go

all: build

build:
	go build -o $(BINARY_NAME) $(SRC)

clean:
	rm -f $(BINARY_NAME)

NixDevShellName:
	@echo "go"

test:
	cd tests && go test -v -timeout 300s .

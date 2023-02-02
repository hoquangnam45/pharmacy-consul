TARGET=pharmacy-consul
BUILD_FOLDER = ./build

all: clean build

clean:
	rm -rf $(BUILD_FOLDER)

build:
	go build -o $(BUILD_FOLDER)/$(TARGET) main.go
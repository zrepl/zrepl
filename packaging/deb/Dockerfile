FROM debian:latest

# binutils are for cross-compilation to work in bullseye
RUN apt-get update && apt-get install -y \
	build-essential \
    devscripts \
    dh-exec \
	binutils-aarch64-linux-gnu \
	binutils-arm-linux-gnueabihf  \
	binutils-i686-linux-gnu \
	binutils-x86-64-linux-gnu

RUN mkdir -p /build/src && chmod -R 0777 /build

WORKDIR /build/src


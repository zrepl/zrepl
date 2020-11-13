FROM debian:latest

RUN apt-get update && apt-get install -y \
	build-essential \
    devscripts \
    dh-exec

RUN mkdir -p /build/src && chmod -R 0777 /build

WORKDIR /build/src


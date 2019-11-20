FROM golang:latest

RUN apt-get update && apt-get install -y \
    python3-pip \
    unzip \
    gawk

ADD build.installprotoc.bash ./
RUN bash build.installprotoc.bash

ADD lazy.sh /tmp/lazy.sh
ADD docs/requirements.txt /tmp/requirements.txt
ENV ZREPL_LAZY_DOCS_REQPATH=/tmp/requirements.txt
RUN /tmp/lazy.sh docdep

# prepare volume mount of git checkout to /zrepl
RUN mkdir -p /src/github.com/zrepl/zrepl
RUN mkdir -p /.cache && chmod -R 0777 /.cache

# $GOPATH is /go
# Go 1.12 doesn't use modules within GOPATH, but 1.13 and later do
# => store source outside of GOPATH
WORKDIR /src

# Install build tools (e.g. protobuf generator, stringer) into $GOPATH/bin
ADD build/ /tmp/build
RUN /tmp/lazy.sh godep

RUN chmod -R 0777 /go


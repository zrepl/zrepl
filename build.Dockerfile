FROM golang:latest

RUN apt-get update && apt-get install -y \
    python3-pip \
    unzip

RUN wget https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-linux-x86_64.zip
RUN echo "6003de742ea3fcf703cfec1cd4a3380fd143081a2eb0e559065563496af27807  protoc-3.6.1-linux-x86_64.zip" | sha256sum -c
RUN unzip -d /usr protoc-3.6.1-linux-x86_64.zip

ADD lazy.sh /tmp/lazy.sh
ADD docs/requirements.txt /tmp/requirements.txt
ENV ZREPL_LAZY_DOCS_REQPATH=/tmp/requirements.txt
RUN /tmp/lazy.sh docdep

# prepare volume mount of git checkout to /zrepl
RUN mkdir -p /go/src/github.com/zrepl/zrepl
RUN mkdir -p /.cache && chmod -R 0777 /.cache
WORKDIR /go/src/github.com/zrepl/zrepl

ADD Gopkg.toml Gopkg.lock  ./

# godep will install the Go dependencies to vendor in order to then build and install
# build dependencies like stringer to $GOPATH/bin.
# However, since users volume-mount their Git checkout into /go/src/github.com/zrepl/zrepl
# the vendor directory will be empty at build time, allowing them to experiment with
# new checkouts, etc.
# Thus, we only use the vendored deps for building dependencies.
RUN /tmp/lazy.sh godep
RUN chmod -R 0777 /go


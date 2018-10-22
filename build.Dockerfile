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
RUN /tmp/lazy.sh devsetup

# prepare volume mount of git checkout to /zrepl
RUN mkdir -p /go/src/github.com/zrepl/zrepl
RUN chmod -R 0777 /go
RUN mkdir -p /.cache && chmod -R 0777 /.cache
WORKDIR /go/src/github.com/zrepl/zrepl



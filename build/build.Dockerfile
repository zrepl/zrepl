FROM !SUBSTITUTED_BY_MAKEFILE

RUN apt-get update && apt-get install -y \
    python3-pip \
    python3-venv \
    unzip \
    gawk

# setup venv for docs
ENV VIRTUAL_ENV=/opt/venv
RUN python3 -m venv $VIRTUAL_ENV
ENV PATH="$VIRTUAL_ENV/bin:$PATH"
ADD docs/requirements.txt /tmp/requirements.txt
RUN pip3 install -r /tmp/requirements.txt

# Go toolchain uses xdg-cache
RUN mkdir -p /.cache && chmod -R 0777 /.cache

# Go devtools are managed by Makefile

WORKDIR /src


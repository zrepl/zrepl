FROM !SUBSTITUTED_BY_MAKEFILE

ARG BUILD_UID=1000
ARG BUILD_GID=1000

RUN apt-get update && apt-get install -y \
    python3 \
    unzip \
    gawk \
    curl

# Create build user with the host's UID/GID before installing uv
RUN groupadd -g ${BUILD_GID} zrepl_build && \
    useradd -u ${BUILD_UID} -g ${BUILD_GID} -m zrepl_build

# Go toolchain uses xdg-cache
RUN mkdir -p /.cache && chmod -R 0777 /.cache

# Install uv as the build user - version from .uv-version file
ADD .uv-version /tmp/.uv-version
USER zrepl_build
RUN UV_VERSION=$(cat /tmp/.uv-version) && \
    curl -LsSf https://astral.sh/uv/${UV_VERSION}/install.sh | sh

# Go devtools are managed by Makefile

WORKDIR /src
ENV PATH="/home/zrepl_build/.local/bin:$PATH" \
    GOCACHE="/.cache/go-build"


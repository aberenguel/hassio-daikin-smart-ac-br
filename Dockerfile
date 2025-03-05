ARG BUILD_FROM
FROM $BUILD_FROM

# Setup base
RUN apk add --no-cache go

# Copy data
COPY rootfs /
COPY app    /app

WORKDIR /app

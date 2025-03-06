ARG BUILD_FROM

FROM $BUILD_FROM as buildStage

# Setup base
RUN apk add --no-cache go

# Copy code
COPY app    /app

# Build
WORKDIR /app
RUN go build -o dist/daikin-server main.go

FROM $BUILD_FROM

# Copy data
COPY rootfs /
COPY --from=buildStage /app/dist/ /app/

WORKDIR /app

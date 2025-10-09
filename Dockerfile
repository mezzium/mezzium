FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

ARG VERSION="0.0.1"
ARG COMMIT_HASH=""

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=$GOARM \
    go build -ldflags "-X main.Version=${VERSION} -X main.CommitHash=${COMMIT_HASH}" \
    -o /out/mezzium ./cmd/app


FROM alpine:3.22

RUN addgroup -S mezzium && adduser -S mezzium -G mezzium

WORKDIR /app

COPY --from=builder /out/mezzium .
COPY configs/networks ./configs/networks

RUN chown -R mezzium:mezzium /app

USER mezzium

EXPOSE 8080

CMD ["./mezzium"]
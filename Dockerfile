# Build:  docker build -t vrotrss .
# Run:    docker run -it --rm -e VROT_URL=https://rss.example.com \
#             -e VROT_USERNAME=alice -e VROT_PASSWORD=api-password vrotrss
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/vrot ./cmd/vrot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/vrot /usr/local/bin/vrot
ENTRYPOINT ["/usr/local/bin/vrot"]

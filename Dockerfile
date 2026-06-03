FROM golang:1.22 AS build
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/free-classrooms-bot ./cmd/locuspocusbot

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/free-classrooms-bot /app/free-classrooms-bot
ENTRYPOINT ["/app/free-classrooms-bot"]

# syntax=docker/dockerfile:1.7
FROM golang:1.26.5-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/patient-dashboard ./cmd/server

FROM scratch
COPY --from=build /out/patient-dashboard /patient-dashboard
USER 65532:65532
EXPOSE 8443
ENTRYPOINT ["/patient-dashboard"]

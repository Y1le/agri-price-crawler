
#build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .

ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

RUN go build -o agri-price-crawler cmd/craw-server/crawserver.go



#run stage
FROM golang:1.25-alpine
WORKDIR /app
COPY --from=builder /app/agri-price-crawler .
EXPOSE 8080
CMD ["/app/agri-price-crawler"]

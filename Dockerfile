
#build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .

ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

RUN go build -o agri-price-crawler cmd/craw-server/crawserver.go


# 1.你需要在项目根目录创建craw.pem和craw-key.pem，craw.yaml文件
# 2.在docker创建redis容器和mysql容器,并连接到craw-server网络
# 3.运行 docker run -p 8080:8080 --network craw-server -v $(pwd)/craw.yaml:/app/craw.yaml --name craw-server agri-price-crawler
#run stage
FROM golang:1.25-alpine
WORKDIR /app
COPY --from=builder /app/agri-price-crawler .
COPY --from=builder /app/craw.pem .
COPY --from=builder /app/craw-key.pem .
RUN mkdir -p /app/var
EXPOSE 8080
CMD ["/app/agri-price-crawler"]

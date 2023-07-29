FROM golang:1.20-alpine

WORKDIR /app

COPY . .

RUN go build -o app

ENTRYPOINT ["./app"]

CMD ["-u", "https://jsonplaceholder.typicode.com/posts", "-n", "1", "-i", "10s", "-t", "10s"]

VOLUME /app/results

FROM golang:1.13-alpine3.12
RUN apk update && apk add git 

#kubectl
ADD https://storage.googleapis.com/kubernetes-release/release/v1.19.3/bin/linux/amd64/kubectl /usr/local/bin/kubectl
RUN chmod +x /usr/local/bin/kubectl

MAINTAINER Gopa Kumar <gopa@nextensio.net>
COPY files /go
WORKDIR /go/src/nextensio/mel
COPY mel .

RUN go get -d -v ./... \
    && go install -v ./... \
    && \rm -r -f /go/pkg/mod \
    && \rm -r -f /go/pkg/sumdb

CMD /go/bin/mel


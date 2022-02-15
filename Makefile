VERSION=latest
NAME=mel
USER=registry.gitlab.com/nextensio/clustermgr
image=$(shell docker images $(USER)/$(NAME):$(VERSION) -q)

.PHONY: all
all: build

.PHONY: build
build:
	rm -r -f files/version
	echo $(VERSION) > files/version
	cp ~/.ssh/gitlab_rsa files/
	docker build -f Dockerfile.build -t $(USER)/$(NAME):$(VERSION) .
	docker create $(USER)/$(NAME):$(VERSION)
	rm files/gitlab_rsa

.PHONY: clean
clean:
	-rm -r -f files/version


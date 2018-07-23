user     := stelcheck
version  := $(shell git tag --points-at $(git rev-parse HEAD))
git_repo := git@github.com:$(user)/prometheus-swarm-discovery.git
hub_repo := $(user)/prometheus-swarm

# Pull dependencies for local development
deps:
	glide install

# Build docker image
build:
	docker build . -t stelcheck/prometheus-swarm

# Run service locally (using docker-compose)
run: build
	docker-compose up

# Release a new version
release: build
ifndef version
	$(error usage: current commit is not tagged, please make sure to tag before releasing)
endif
	git push --tags $(git_repo) master
	docker login
	docker tag $(hub_repo):latest $(hub_repo):$(version)
	docker push $(hub_repo):$(version)

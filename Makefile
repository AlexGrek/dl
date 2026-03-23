.PHONY: build frontend backend test clean docker helm-install helm-uninstall deploy

BINARY   := dl
IMAGE    := grekodocker/dl
PLATFORM := linux/amd64

build: frontend backend

frontend:
	cd dl-frontend && npm run build
	rm -rf src/static
	cp -r dl-frontend/dist src/static

backend:
	go build -o $(BINARY) ./src/

test:
	go test ./src/...

clean:
	rm -rf $(BINARY) src/static dl-frontend/dist

docker:
	docker buildx build --platform $(PLATFORM) -t $(IMAGE) --push .

helm-install:
	@test -f .secrets.yaml || { echo "ERROR: .secrets.yaml not found"; exit 1; }
	helm upgrade --install dl ./dl-chart --create-namespace --namespace dl \
		-f .secrets.yaml

helm-uninstall:
	helm uninstall dl --namespace dl

deploy:
	@test -f .secrets.yaml || { echo "ERROR: .secrets.yaml not found"; exit 1; }
	$(eval TAG := $(shell git rev-parse --short HEAD))
	docker buildx build --platform $(PLATFORM) -t $(IMAGE):$(TAG) --push .
	helm upgrade --install dl ./dl-chart --create-namespace --namespace dl \
		--set image.tag=$(TAG) \
		-f .secrets.yaml

run: backend
	./$(BINARY) -secrets .secrets.yaml

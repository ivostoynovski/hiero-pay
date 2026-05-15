.PHONY: build build-frontend dev-frontend dev-backend clean install-frontend

build: build-frontend
	go build .

build-frontend:
	cd web && npm run build

dev-backend:
	go run . serve

dev-frontend:
	cd web && npm run dev

install-frontend:
	cd web && npm install

clean:
	rm -f hiero-pay
	rm -rf web/dist

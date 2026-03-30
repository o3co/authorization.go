.PHONY: test
test:
	docker compose up -d --wait
	go test -tags integration -count=1 -v ./... ; \
	status=$$? ; \
	docker compose down ; \
	exit $$status

.PHONY: clean
clean:
	docker compose down -v

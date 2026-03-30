.PHONY: test
test:
	docker compose up -d
	@echo "Waiting for OPA..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		curl -sf http://localhost:8181/health > /dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "Waiting for Cedar agent..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		curl -sf http://localhost:8180/health > /dev/null 2>&1 && break; \
		sleep 1; \
	done
	go test -tags integration -count=1 -v ./... ; \
	status=$$? ; \
	docker compose down ; \
	exit $$status

.PHONY: clean
clean:
	docker compose down -v

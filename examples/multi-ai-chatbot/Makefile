.PHONY: run-alice run-bob run help

help:
	@echo "Multi-AI Chatbot Example"
	@echo ""
	@echo "Available commands:"
	@echo "  make run-alice    - Start chatbot as Alice AI"
	@echo "  make run-bob      - Start chatbot as Bob AI"
	@echo "  make run user=<name> - Start chatbot with custom AI identity"
	@echo ""
	@echo "Prerequisites:"
	@echo "  1. PCAS server must be running (make dev-up in PCAS root)"
	@echo "  2. OpenAI API key must be configured in .env"

run-alice:
	go run main.go --user-id alice

run-bob:
	go run main.go --user-id bob

run:
	@if [ -z "$(user)" ]; then \
		echo "Error: Please specify user with 'make run user=<name>'"; \
		exit 1; \
	fi
	go run main.go --user-id $(user)
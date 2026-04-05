COMPOSE := docker compose

.PHONY: build up down logs ps restart

build:
	$(COMPOSE) build --no-cache

up:
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

restart:
	$(COMPOSE) down
	$(COMPOSE) up --build -d

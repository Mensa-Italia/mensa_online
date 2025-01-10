# Mensa app DB
[![Build](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml/badge.svg)](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml)

This is the base repository for the Mensa app database. It contains the database schema and the data for the Mensa app.

We will use this database to handle SIG (Special Interest Group), Events, Local groups, and other data that is not handled by the main mensa servers.

## How to use
### Docker
The easiest way to run the database is to use docker.
You can copy and paste the compose.yaml file into portainer and set up all the env variables.

This will start the database and expose it on port 8080. You will have to create your own credentials for the database.

### Manual
You can also run the database manually. Just download this repo and run main/main.go with the following command
```bash
go run main/main.go serve
```

## ENV variables
The following ENV variables are required to run the database:

| Name | Description                |
|------|----------------------------|
|PASSWORD_UUID| The uuid used into uuid v5 |
|PASSWORD_SALT| The salt used to hash the password |
|PASSWORD_SALT| The salt used to hash the password |



## Notes
This doc will be updated as the project progresses. 

CREATE TABLE stations (
    id BINARY(16) NOT NULL,
    station_code VARCHAR(20) NOT NULL,
    station_name VARCHAR(100) NOT NULL,
    latitude DECIMAL(9,6) NOT NULL,
    longitude DECIMAL(9,6) NOT NULL,
    region VARCHAR(100) DEFAULT NULL,
    data_source VARCHAR(50) DEFAULT NULL,
    status_active TINYINT(1) NOT NULL DEFAULT 1,
    created_by BINARY(16) DEFAULT NULL,
    updated_by BINARY(16) DEFAULT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT pk_stations PRIMARY KEY (id),
    CONSTRAINT uq_stations_code UNIQUE (station_code),
    CONSTRAINT fk_stations_created_by FOREIGN KEY (created_by)
        REFERENCES users (id)
        ON UPDATE CASCADE
        ON DELETE SET NULL,
    CONSTRAINT fk_stations_updated_by FOREIGN KEY (updated_by)
        REFERENCES users (id)
        ON UPDATE CASCADE
        ON DELETE SET NULL
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE weather_predictions (
    id BINARY(16) NOT NULL,
    user_id BINARY(16) NOT NULL,
    station_id BINARY(16) NOT NULL,
    prediction_date DATETIME NOT NULL,
    temperature DECIMAL(5,2) NOT NULL,
    humidity DECIMAL(5,2) NOT NULL,
    rainfall DECIMAL(6,2) NOT NULL,
    air_pressure DECIMAL(6,2) NOT NULL,
    wind_speed DECIMAL(5,2) DEFAULT NULL,
    wind_direction DECIMAL(5,2) DEFAULT NULL,

    -- Tambahan Kolom Klimatologi El Niño & La Niña
    enso_index DECIMAL(4,2) DEFAULT NULL COMMENT 'Indeks ONI / Nino 3.4',
    climate_phase VARCHAR(20) DEFAULT NULL COMMENT 'Contoh: El Nino, La Nina, Netral',

    status_weather VARCHAR(50) NOT NULL,
    confidence_score DECIMAL(5,2) DEFAULT NULL COMMENT 'Confidence per-prediksi dari MC Dropout, dihitung ulang tiap request',
    uncertainty_metric DECIMAL(6,4) DEFAULT NULL COMMENT 'Standar deviasi suhu (°C) hasil MC Dropout -- ukuran ketidakstabilan model per-prediksi, BUKAN RMSE klasik terhadap data aktual',
    model_version VARCHAR(50) DEFAULT NULL,
    prediction_horizon INT DEFAULT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT pk_weather_predictions PRIMARY KEY (id),
    CONSTRAINT fk_weather_predictions_user FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON UPDATE CASCADE
        ON DELETE CASCADE,
    CONSTRAINT fk_weather_predictions_station FOREIGN KEY (station_id)
        REFERENCES stations (id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;
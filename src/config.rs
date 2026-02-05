use crate::storage;
use std::{env, path::PathBuf};
use dotenvy::dotenv;
use tracing::{info, error, warn}; 

pub struct Config {
    pub storage_path: PathBuf,
    pub server_port: u16,
    pub auth_username: String,
    pub auth_password: String,
}

impl Config {
    pub fn load() -> Self {

        // Load .env
        if let Err(_) = dotenv() {
            warn!(".env file not found, falling back to system environment variables");
        }

        //  intitialise Storage Path
        let storage_path = match storage::init() {
            Ok(path) => path,
            Err(e) => {
                error!("Configuration Error: {}", e);
                std::process::exit(1);
            }
        };

        // load the Server Port from env
        let server_port_str = env::var("PORT").unwrap_or_else(|_| {
            warn!("PORT missing in .env, defaulting to 8080");
            "8080".to_string()
        });

        let server_port: u16 = server_port_str.parse().unwrap_or_else(|_| {
            error!("Invalid PORT '{}': must be a number", server_port_str);
            std::process::exit(1);
        });

        // load the Auth Credentials
        let auth_username = env::var("AUTH_USERNAME").unwrap_or_else(|_| {
            error!("Login Error: AUTH_USERNAME missing in environment");
            std::process::exit(1);
        });

        let auth_password = env::var("AUTH_PASSWORD").unwrap_or_else(|_| {
            error!("Login Error: AUTH_PASSWORD missing in environment");
            std::process::exit(1);
        });

        // Success Log
        info!("Successfully loaded configuration for user: {}", auth_username);

        Self {
            storage_path,
            server_port,
            auth_username,
            auth_password,
        }
    }
}
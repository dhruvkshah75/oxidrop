use crate::storage;

use std::{env, path::PathBuf};
use dotenvy::dotenv;


pub struct Config {
    pub storage_path: PathBuf,
    pub server_port: u16,
    pub auth_username: String,
    pub auth_password: String,
}

// implementation Container of the config Struct 
impl Config {

    pub fn load() -> Self {

        // loads the env var and .expect(): if dotenv fails then it will crash and prompt this message 
        dotenv().expect("Critical Error: .env file not found!");

        // calling the storage init function to load the storage path
        let storage_path = match storage::init() {
            Ok(path) => path,
            Err(e) => {
                // If storage fails, we can't start the app.
                eprintln!("Configuration Error: {}", e);
                std::process::exit(1);
            }
        };

        // we load the server port 
        let server_port_str = match env::var("PORT") {
            Ok(port) => port,
            Err(e) => {
                eprintln!("Configuration Error Port Missing: {}", e);
                std::process::exit(1);
            }
        };


        // load the auth_username => required 
        let auth_username = match env::var("AUTH_USERNAME") {
            Ok(auth_username) => auth_username,
            Err(e) => {
                eprintln!("Login Error Username Missing: {}", e);
                std::process::exit(1);
            }
        };

        // load the password => required to verify 
        let auth_password = match env::var("AUTH_PASSWORD") {
            Ok(auth_password) => auth_password,
            Err(e) => {
                eprintln!("Login error password missing: {}", e);
                std::process::exit(1);
            }
        };


        let server_port: u16 = server_port_str
            .parse()
            .expect("PORT in .env must be a valid number between 0 and 65535");

        
        


        // Return the completed Config struct
        Self {
            storage_path,
            server_port,
            auth_username,
            auth_password
        }
    }
}

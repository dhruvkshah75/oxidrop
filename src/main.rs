use crate::config::Config;
use std::sync::Arc;

mod config;
mod storage;
mod server;
mod dns;

// we need to allow shared ownership of the config struct so that multiple handlers can use it 

#[tokio::main]
async fn main() {

    let name = String::from("oxidrop");
    println!("Welcome to {}", name);

    let version = String::from("1.0.0");
    println!("{} version {v}", name, v = version);


    // calling the storage init function
    let config = Config::load();

    println!("mDNS active: Connect at http://oxidrop.local:{}", config.server_port);
    println!("Storage initialized at: {:?}", config.storage_path);

    let _mdns_handle = dns::start_responder(config.server_port);
    
    // Wrap the config in an arc to allow multiple handlers to use it 
    let shared_config = Arc::new(config);  // beyond this config cannot be used: Rust Ownership model

    
    server::start(shared_config).await;

}

use crate::config::Config;
use std::sync::Arc;
use tracing::info;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

mod config;
mod storage;
mod server;
mod dns;

// we need to allow shared ownership of the config struct so that multiple handlers can use it 

#[tokio::main]
async fn main() {
    // Initialize logging with tower_http traces enabled
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "oxidrop=info,tower_http=trace,tower_http::trace=trace".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();

    let name = String::from("oxidrop");
    info!("Welcome to {}", name);

    let version = String::from("1.0.0");
    info!("{} version {}", name, version);

    // calling the storage init function
    let config = Config::load();

    info!("mDNS active: Connect at http://oxidrop.local:{}", config.server_port);
    info!("Storage initialized at: {:?}", config.storage_path);

    let _mdns_handle = dns::start_responder(config.server_port);
    
    // Wrap the config in an arc to allow multiple handlers to use it 
    let shared_config = Arc::new(config);  // beyond this config cannot be used: Rust Ownership model

    server::start(shared_config).await;

}

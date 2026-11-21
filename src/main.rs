use std::{error::Error, process::ExitCode, sync::Arc};
use tokio::{
    io::AsyncReadExt,
    net::UnixStream,
    signal::unix::{Signal, SignalKind, signal},
};

mod config;
mod dns;
mod server;
mod smb;
mod storage;

type Result<T> = std::result::Result<T, Box<dyn Error>>;

struct Shutdown {
    interrupt: Signal,
    terminate: Signal,
    supervisor: Option<UnixStream>,
}

impl Shutdown {
    async fn wait(&mut self) {
        tokio::select! {
            _ = self.interrupt.recv() => {}
            _ = self.terminate.recv() => {}
            _ = async {
                if let Some(socket) = self.supervisor.as_mut() {
                    let _ = socket.read(&mut [0]).await;
                } else {
                    std::future::pending::<()>().await;
                }
            } => {}
        }
    }
}

#[tokio::main]
async fn main() -> ExitCode {
    let args = match config::Args::parse() {
        Ok(Some(args)) => args,
        Ok(None) => {
            println!("Usage: oxidrop [--share-dir PATH] [--smb] [--quiet]");
            return ExitCode::SUCCESS;
        }
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    tracing_subscriber::fmt()
        .with_writer(std::io::stderr)
        .with_ansi(!args.quiet)
        .with_env_filter(if args.quiet {
            "off".into()
        } else {
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "oxidrop=info,tower_http=trace".into())
        })
        .init();

    let quiet = args.quiet;
    match run(args).await {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            let needs_root = error.downcast_ref::<smb::NeedsRoot>().is_some();
            if quiet {
                eprintln!(
                    "{}",
                    serde_json::json!({
                        "event": if needs_root { "privilege_required" } else { "error" },
                        "message": error.to_string(),
                    })
                );
            } else {
                eprintln!("{error}");
            }
            ExitCode::from(if needs_root { 77 } else { 1 })
        }
    }
}

async fn run(args: config::Args) -> Result<()> {
    let mut shutdown = Shutdown {
        interrupt: signal(SignalKind::interrupt())?,
        terminate: signal(SignalKind::terminate())?,
        supervisor: match args.supervisor_socket.as_ref() {
            Some(path) => Some(UnixStream::connect(path).await?),
            None => None,
        },
    };
    if args.smb {
        return smb::run(args, &mut shutdown).await;
    }

    let config = Arc::new(config::Config::load(args.share_dir)?);
    let listener = tokio::net::TcpListener::bind(("0.0.0.0", config.server_port)).await?;
    let port = listener.local_addr()?.port();
    let mdns = dns::Advertisement::start("_http._tcp.local.", port)?;
    if args.quiet {
        println!(
            "{}",
            serde_json::json!({"event": "ready", "url": format!("http://oxidrop.local:{port}"), "share_dir": config.storage_path.to_string_lossy()})
        );
    } else {
        tracing::info!("WebDAV listening at http://oxidrop.local:{port}");
    }
    let result = server::start(config, listener, async move { shutdown.wait().await }).await;
    mdns.stop().await;
    Ok(result?)
}

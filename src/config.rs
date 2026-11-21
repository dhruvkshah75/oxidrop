use std::{env, ffi::OsString, path::PathBuf};

#[derive(Default)]
pub struct Args {
    pub share_dir: Option<PathBuf>,
    pub smb: bool,
    pub managed_smb: bool,
    pub quiet: bool,
    pub supervisor_socket: Option<PathBuf>,
}

impl Args {
    pub fn parse() -> Result<Option<Self>, String> {
        let mut args = env::args_os().skip(1);
        let mut options = Self::default();
        while let Some(arg) = args.next() {
            match arg.to_str() {
                Some("--share-dir") => {
                    let path = args.next().filter(|path| {
                        !path.is_empty() && !path.as_encoded_bytes().starts_with(b"--")
                    });
                    options.share_dir = Some(path.ok_or("--share-dir requires a path")?.into());
                }
                Some("--supervisor-socket") => {
                    options.supervisor_socket = Some(
                        args.next()
                            .ok_or("--supervisor-socket requires a path")?
                            .into(),
                    );
                }
                Some("--smb") => options.smb = true,
                Some("--managed-smb") => {
                    options.smb = true;
                    options.managed_smb = true;
                }
                Some("--quiet") => options.quiet = true,
                Some("--help" | "-h") => return Ok(None),
                _ => return Err(format!("Unknown argument: {}", arg.to_string_lossy())),
            }
        }
        Ok(Some(options))
    }
}

pub struct Config {
    pub storage_path: PathBuf,
    pub server_port: u16,
    pub auth_username: String,
    pub auth_password: String,
}

impl Config {
    pub fn load(share_dir: Option<PathBuf>) -> Result<Self, Box<dyn std::error::Error>> {
        dotenvy::dotenv().ok();
        let storage_path = crate::storage::init(
            share_dir.or_else(|| env::var_os("STORAGE_PATH").map(PathBuf::from)),
            env::var_os("HOME").map(PathBuf::from),
        )?;
        Ok(Self {
            storage_path,
            server_port: env::var("PORT").unwrap_or_else(|_| "8080".into()).parse()?,
            auth_username: env::var("AUTH_USERNAME")
                .map_err(|_| "AUTH_USERNAME is required for WebDAV")?,
            auth_password: env::var("AUTH_PASSWORD")
                .map_err(|_| "AUTH_PASSWORD is required for WebDAV")?,
        })
    }
}

pub fn sudo_uid() -> Option<OsString> {
    // SAFETY: geteuid has no preconditions.
    (unsafe { libc::geteuid() } == 0)
        .then(|| env::var_os("SUDO_UID"))
        .flatten()
}

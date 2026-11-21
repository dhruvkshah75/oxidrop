use crate::{Result, Shutdown, config, dns, storage};
use std::{
    env, fs,
    io::{self, Read, Write},
    os::unix::{
        fs::{DirBuilderExt, OpenOptionsExt},
        process::CommandExt,
    },
    path::{Path, PathBuf},
    process::Stdio,
    time::Duration,
};
use tokio::{
    io::AsyncWriteExt,
    net::TcpStream,
    process::{Child, Command},
    time,
};

const PORT: u16 = 445;
const CONFIG: &str = "/tmp/oxidrop-smb.conf";
const SYSTEM_CONFIG: &str = "/etc/samba/smb.conf";

#[derive(Debug)]
pub struct NeedsRoot;

impl std::fmt::Display for NeedsRoot {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("Standalone Samba needs root to bind port 445 and switch to the share owner. Run oxidrop via sudo or configure the system [oxidrop] Samba share.")
    }
}

impl std::error::Error for NeedsRoot {}

struct Account {
    name: String,
    home: PathBuf,
}

impl Account {
    async fn from_sudo() -> Result<Self> {
        let uid = config::sudo_uid()
            .and_then(|uid| uid.to_str()?.parse::<u32>().ok())
            .filter(|uid| *uid != 0)
            .ok_or("Start standalone SMB with sudo from the non-root share owner's account")?;
        let output = Command::new("getent")
            .args(["passwd", &uid.to_string()])
            .output()
            .await?;
        if !output.status.success() {
            return Err(format!("No local account for UID {uid}").into());
        }
        let entry = std::str::from_utf8(&output.stdout)?;
        let fields: Vec<_> = entry.trim_end_matches('\n').split(':').collect();
        if fields.len() != 7
            || !fields[0]
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || b"._-$".contains(&c))
            || fields[0].is_empty()
        {
            return Err("Unsupported local Samba account".into());
        }
        Ok(Self {
            name: fields[0].into(),
            home: fields[5].into(),
        })
    }

    async fn prepare_share(&self, path: &Path) -> Result<PathBuf> {
        let status = Command::new("runuser")
            .args([
                "-u",
                &self.name,
                "--",
                "sh",
                "-c",
                "umask 077; mkdir -p -- \"$1\" && test -w \"$1\" && test -x \"$1\"",
                "oxidrop",
            ])
            .arg(path)
            .status()
            .await?;
        if !status.success() {
            return Err(format!("{} cannot write to {}", self.name, path.display()).into());
        }
        Ok(path.canonicalize()?)
    }
}

struct Files {
    runtime: PathBuf,
    owns_config: bool,
}

impl Files {
    fn create(path: &Path, account: &str) -> Result<Self> {
        let runtime = PathBuf::from(format!("/tmp/oxidrop-smb-{}", std::process::id()));
        fs::DirBuilder::new().mode(0o700).create(&runtime)?;
        let mut files = Self {
            runtime,
            owns_config: false,
        };
        let config = standalone_config(path, &files.runtime, account)?;
        let mut file = fs::OpenOptions::new().write(true).create_new(true)
            .mode(0o600).open(CONFIG)
            .map_err(|error| format!("Cannot claim {CONFIG}: {error}. Check whether another oxidrop instance is running."))?;
        files.owns_config = true;
        file.write_all(config.as_bytes())?;
        Ok(files)
    }
}

impl Drop for Files {
    fn drop(&mut self) {
        if self.owns_config {
            let _ = fs::remove_file(CONFIG);
        }
        let _ = fs::remove_dir_all(&self.runtime);
    }
}

fn standalone_config(path: &Path, runtime: &Path, account: &str) -> Result<String> {
    let path = path.to_str().ok_or("Samba requires a UTF-8 share path")?;
    if path.contains(['\n', '\r', '\0', '%']) || path.ends_with('\\') || path.trim_end() != path {
        return Err(
            "The share path contains characters Samba would interpret as configuration syntax"
                .into(),
        );
    }
    let runtime = runtime.display();
    Ok(format!(
        "[global]
    server role = standalone server
    netbios name = OXIDROP
    smb ports = 445
    server min protocol = SMB2_02
    vfs objects = fruit streams_xattr
    fruit:metadata = stream
    fruit:nfs_aces = no
    fruit:aapl = yes
    multicast dns register = no
    load printers = no
    disable spoolss = yes
    usershare max shares = 0
    private dir = {runtime}
    state directory = {runtime}
    cache directory = {runtime}
    lock directory = {runtime}
    pid directory = {runtime}
    ncalrpc dir = {runtime}/ncalrpc
    log file = {runtime}/smbd.log
    passdb backend = tdbsam

[oxidrop]
    path = {path}
    browseable = yes
    read only = no
    guest ok = no
    valid users = {account}
    follow symlinks = no
    veto files = /.oxidrop-transfer-*/
    create mask = 0600
    directory mask = 0700
"
    ))
}

async fn provision_user(account: &str) -> Result<String> {
    let mut random = [0; 16];
    fs::File::open("/dev/urandom")?.read_exact(&mut random)?;
    let password: String = random.iter().map(|byte| format!("{byte:02x}")).collect();
    let mut command = Command::new("smbpasswd")
        .args(["-c", CONFIG, "-s", "-a", account])
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .kill_on_drop(true)
        .spawn()?;
    if let Some(mut input) = command.stdin.take() {
        input
            .write_all(format!("{password}\n{password}\n").as_bytes())
            .await?;
    }
    if !command.wait().await?.success() {
        return Err("smbpasswd could not initialize the standalone account".into());
    }
    Ok(password)
}

async fn parameter(section: &str, name: &str) -> Result<String> {
    let output = Command::new("testparm")
        .args([
            "-s",
            "--section-name",
            section,
            "--parameter-name",
            name,
            SYSTEM_CONFIG,
        ])
        .output()
        .await?;
    let value = std::str::from_utf8(&output.stdout)?.trim();
    if !output.status.success() || value.is_empty() {
        return Err(format!("Unable to read {name} in [{section}] from {SYSTEM_CONFIG}").into());
    }
    Ok(value.into())
}

async fn check_system_share(path: &Path) -> Result<()> {
    let active = ["smbd.service", "smb.service"];
    let output = Command::new("systemctl")
        .args(["is-active", "--quiet"])
        .args(active)
        .output()
        .await?;
    if !output.status.success() {
        return Err("TCP 445 is occupied and no system Samba service (smbd.service or smb.service) is active".into());
    }
    let actual = parameter("oxidrop", "path").await.map_err(|error| {
        format!(
            "{error}. Configure the [oxidrop] share with path = {} and reload system Samba.",
            path.display()
        )
    })?;
    if Path::new(&actual).canonicalize()? != path {
        return Err(format!(
            "System Samba [oxidrop] exports {actual}; expected {}",
            path.display()
        )
        .into());
    }
    if parameter("oxidrop", "read only").await? != "No"
        || !parameter("oxidrop", "vfs objects")
            .await?
            .split_whitespace()
            .collect::<Vec<_>>()
            .windows(2)
            .any(|pair| pair == ["fruit", "streams_xattr"])
        || parameter("oxidrop", "fruit:metadata").await? != "stream"
        || parameter("global", "fruit:nfs_aces").await? != "no"
        || !matches!(
            parameter("global", "server min protocol").await?.get(..4),
            Some("SMB2" | "SMB3")
        )
    {
        return Err("System Samba [oxidrop] must be read/write and have the SMB2+ and Apple VFS settings in the README".into());
    }
    Ok(())
}

async fn listening() -> bool {
    let addresses = if_addrs::get_if_addrs().unwrap_or_default();
    let mut probes = tokio::task::JoinSet::new();
    for address in std::iter::once(std::net::IpAddr::V4(std::net::Ipv4Addr::LOCALHOST)).chain(
        addresses
            .iter()
            .filter(|addr| !addr.is_loopback())
            .map(|addr| addr.ip()),
    ) {
        probes.spawn(async move {
            time::timeout(
                Duration::from_millis(250),
                TcpStream::connect((address, PORT)),
            )
            .await
            .is_ok_and(|result| result.is_ok())
        });
    }
    while let Some(result) = probes.join_next().await {
        if matches!(result, Ok(true)) {
            return true;
        }
    }
    false
}

async fn stop_child(child: &mut Child, pid: Option<u32>) {
    if let Some(pid) = pid {
        // SAFETY: a negative child PID addresses its dedicated Samba process group.
        unsafe { libc::kill(-(pid as i32), libc::SIGTERM) };
        if !matches!(
            time::timeout(Duration::from_secs(3), child.wait()).await,
            Ok(Ok(_))
        ) {
            unsafe { libc::kill(-(pid as i32), libc::SIGKILL) };
            let _ = child.wait().await;
        }
    }
}

pub async fn run(args: config::Args, shutdown: &mut Shutdown) -> Result<()> {
    if listening().await {
        if args.managed_smb || args.supervisor_socket.is_some() {
            return Err("TCP 445 is already in use. The TUI needs its own Samba instance so it can close the share on exit. Stop the service using port 445, then reopen oxidrop-tui.".into());
        }
        let home = match config::sudo_uid() {
            Some(_) => Some(Account::from_sudo().await?.home),
            None => env::var_os("HOME").map(PathBuf::from),
        };
        let path = storage::share_path(args.share_dir, home)?.canonicalize()?;
        check_system_share(&path).await?;
        return advertise(&path, args.quiet, None, None, shutdown).await;
    }

    // SAFETY: geteuid has no preconditions.
    if unsafe { libc::geteuid() } != 0 {
        return Err(NeedsRoot.into());
    }
    let account = Account::from_sudo().await?;
    let path = storage::share_path(args.share_dir, Some(account.home.clone()))?;
    let path = account.prepare_share(&path).await?;
    let files = Files::create(&path, &account.name)?;
    let password = tokio::select! {
        biased;
        _ = shutdown.wait() => return Ok(()),
        result = provision_user(&account.name) => result?,
    };
    let mut command = std::process::Command::new("smbd");
    command
        .args(["-F", "--no-process-group", "-s", CONFIG])
        .process_group(0);
    let mut child = Command::from(command)
        .kill_on_drop(true)
        .spawn()
        .map_err(|error| {
            format!("Cannot start smbd (install Samba and its VFS modules): {error}")
        })?;
    let pid = child.id();
    let result = async {
        let ready = async {
            while !listening().await {
                time::sleep(Duration::from_millis(100)).await;
            }
        };
        tokio::select! {
            biased;
            _ = shutdown.wait() => return Ok(()),
            status = child.wait() => return Err(format!("smbd exited before it was ready: {}", status?).into()),
            result = time::timeout(Duration::from_secs(8), ready) => result.map_err(|_| "smbd did not open TCP 445")?,
        }
        advertise(&path, args.quiet, Some((&account.name, &password)), Some(&mut child), shutdown).await
    }.await;
    stop_child(&mut child, pid).await;
    if result.is_err()
        && let Ok(log) = fs::read_to_string(files.runtime.join("smbd.log"))
    {
        for line in log
            .lines()
            .rev()
            .take(16)
            .collect::<Vec<_>>()
            .into_iter()
            .rev()
        {
            eprintln!("{line}");
        }
    }
    result
}

async fn advertise(
    path: &Path,
    quiet: bool,
    credentials: Option<(&str, &str)>,
    child: Option<&mut Child>,
    shutdown: &mut Shutdown,
) -> Result<()> {
    let mdns = dns::Advertisement::start("_smb._tcp.local.", PORT)?;
    let url = "smb://oxidrop.local";
    let announcement = if quiet {
        let (username, password) = credentials.unzip();
        serde_json::json!({
            "event": "ready",
            "url": url,
            "share_dir": path.to_string_lossy(),
            "username": username,
            "password": password,
        })
        .to_string()
    } else {
        let mut message = format!("SMB share is ready at {url} (oxidrop): {}", path.display());
        if let Some((username, password)) = credentials {
            message.push_str(&format!(
                "\nRegistered User: {username}\nSession password: {password}"
            ));
        }
        message
    };
    if let Err(error) = writeln!(io::stdout(), "{announcement}") {
        mdns.stop().await;
        return Err(error.into());
    }
    let monitor = async {
        if let Some(child) = child {
            return Err(format!("smbd exited: {}", child.wait().await?).into());
        }
        loop {
            time::sleep(Duration::from_secs(3)).await;
            if !listening().await {
                return Err("System Samba stopped listening on TCP 445".into());
            }
        }
    };
    let result = tokio::select! {
        _ = shutdown.wait() => Ok(()),
        result = monitor => result,
    };
    mdns.stop().await;
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn standalone_config_keeps_share_paths_literal() {
        let runtime = Path::new("/tmp/test");
        let config = standalone_config(Path::new("/home/me/my share"), runtime, "me").unwrap();
        assert!(config.contains("path = /home/me/my share\n"));
        assert!(config.contains("fruit:nfs_aces = no"));
        for unsafe_path in ["/tmp/a\n[share]", "/tmp/a%", "/tmp/a\\", "/tmp/a "] {
            assert!(standalone_config(Path::new(unsafe_path), runtime, "me").is_err());
        }
    }
}

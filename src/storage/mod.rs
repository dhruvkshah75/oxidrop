use std::{fs, io, path::PathBuf};

pub fn share_path(path: Option<PathBuf>, home: Option<PathBuf>) -> io::Result<PathBuf> {
    path.or_else(|| home.map(|home| home.join("oxidrop-share")))
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "set HOME or pass --share-dir"))
}

pub fn init(path: Option<PathBuf>, home: Option<PathBuf>) -> io::Result<PathBuf> {
    let path = share_path(path, home)?;
    fs::create_dir_all(&path)?;
    path.canonicalize()
}

use mdns_sd::{ServiceDaemon, ServiceInfo};
use std::{collections::HashMap, time::Duration};
use tracing::warn;

pub struct Advertisement {
    daemon: ServiceDaemon,
    name: String,
}

impl Advertisement {
    pub fn start(service_type: &str, port: u16) -> Result<Self, mdns_sd::Error> {
        let service = ServiceInfo::new(
            service_type,
            "oxidrop",
            "oxidrop.local.",
            "",
            port,
            HashMap::new(),
        )?
        .enable_addr_auto();
        let daemon = ServiceDaemon::new()?;
        let name = service.get_fullname().to_owned();
        if let Err(error) = daemon.register(service) {
            let _ = daemon.shutdown();
            return Err(error);
        }
        Ok(Self { daemon, name })
    }

    pub async fn stop(self) {
        if let Ok(status) = self.daemon.unregister(&self.name)
            && tokio::time::timeout(Duration::from_secs(1), status.recv_async())
                .await
                .is_err()
        {
            warn!("mDNS unregister timed out");
        }
        if let Ok(status) = self.daemon.shutdown() {
            let _ = tokio::time::timeout(Duration::from_secs(1), status.recv_async()).await;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use mdns_sd::ServiceEvent;

    #[tokio::test]
    #[ignore = "requires a multicast-capable network interface"]
    async fn smb_service_resolves_and_unregisters() {
        let service_type = "_smb._tcp.local.";
        let browser = ServiceDaemon::new().unwrap();
        let events = browser.browse(service_type).unwrap();
        let advertisement = Advertisement::start(service_type, 445).unwrap();
        let discovered = tokio::time::timeout(Duration::from_secs(8), async {
            loop {
                if let ServiceEvent::ServiceResolved(resolved) = events.recv_async().await.unwrap()
                    && resolved.get_fullname() == "oxidrop._smb._tcp.local."
                    && resolved.get_hostname() == "oxidrop.local."
                    && resolved.get_port() == 445
                    && !resolved.get_addresses().is_empty()
                {
                    return;
                }
            }
        })
        .await;
        advertisement.stop().await;
        discovered.expect("SMB service did not resolve");

        tokio::time::timeout(Duration::from_secs(5), async {
            loop {
                if let ServiceEvent::ServiceRemoved(_, name) = events.recv_async().await.unwrap()
                    && name == "oxidrop._smb._tcp.local."
                {
                    return;
                }
            }
        })
        .await
        .expect("SMB service did not send a goodbye");
        browser.shutdown().unwrap().recv_async().await.unwrap();
    }
}

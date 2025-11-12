// URL keyword blocking
const urlKeywords = ['gambling', 'casino', 'porn', 'xxx'];

browser.webRequest.onBeforeRequest.addListener(
  function(details) {
    const url = details.url.toLowerCase();
    
    for (let keyword of urlKeywords) {
      if (url.includes(keyword)) {
          console.log("Found ", keyword, " in ", url);
        // Report to glocker
        fetch('http://127.0.0.1/report', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            url: details.url,
            trigger: `url-keyword:${keyword}`,
            timestamp: Date.now()
          })
        }).catch(() => {}); // Ignore failures
        
        return {cancel: true}; // Block request
      }
    }
  },
  {urls: ["<all_urls>"]},
  ["blocking"]
);

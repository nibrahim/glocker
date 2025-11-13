// URL keyword blocking - will be populated from server
let urlKeywords = ['gambling', 'casino', 'porn', 'xxx']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  try {
    const response = await fetch('http://127.0.0.1/keywords');
    if (response.ok) {
      const data = await response.json();
      if (data.url_keywords && Array.isArray(data.url_keywords)) {
        urlKeywords = data.url_keywords;
        console.log('Updated URL keywords from server:', urlKeywords);
      }
      return data;
    }
  } catch (error) {
    console.log('Failed to fetch keywords from server, using defaults:', error);
  }
  return null;
}

// Initialize keywords on startup
fetchKeywords();

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

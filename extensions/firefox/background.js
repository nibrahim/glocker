// Keyword storage - will be populated from server
let urlKeywords = ['gambling', 'casino', 'porn', 'xxx']; // fallback defaults
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Cached compiled regex patterns for performance
let urlKeywordRegexes = [];
let contentKeywordRegexes = [];

// Global cleanup state for background script
let backgroundCleanedUp = false;

// Status tracking
let glockerConnected = false;

// Compile keywords into regex patterns for performance
function compileKeywordRegexes() {
  // Compile URL keyword regexes
  urlKeywordRegexes = urlKeywords.map(keyword => {
    const escapedKeyword = keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return {
      keyword: keyword,
      regex: new RegExp('\\b' + escapedKeyword + '\\b', 'i')
    };
  });
  
  // Compile content keyword regexes
  contentKeywordRegexes = contentKeywords.map(keyword => {
    const escapedKeyword = keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return {
      keyword: keyword,
      regex: new RegExp('\\b' + escapedKeyword + '\\b', 'i')
    };
  });
  
  console.log('Compiled regex patterns for', urlKeywordRegexes.length, 'URL keywords and', contentKeywordRegexes.length, 'content keywords');
}

// Update browser action icon based on connection status
function updateStatusIcon() {
  const title = glockerConnected ? 'Glocker: Active' : 'Glocker: Disconnected';
  const badgeText = glockerConnected ? '●' : '○';
  const badgeColor = glockerConnected ? '#4CAF50' : '#F44336'; // Green for active, red for disconnected
  
  browser.browserAction.setTitle({ title: title });
  browser.browserAction.setBadgeText({ text: badgeText });
  browser.browserAction.setBadgeBackgroundColor({ color: badgeColor });
}

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
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated content keywords from server:', contentKeywords);
      }
      
      // Recompile regex patterns with new keywords
      compileKeywordRegexes();
      
      // Update connection status
      glockerConnected = true;
      updateStatusIcon();
      
      // Broadcast updated keywords to all content scripts
      broadcastKeywordsToContentScripts();
      
      return data;
    } else {
      glockerConnected = false;
      updateStatusIcon();
    }
  } catch (error) {
    console.log('Failed to fetch keywords from server, using defaults:', error);
    glockerConnected = false;
    updateStatusIcon();
  }
  return null;
}

// Broadcast keywords to all content scripts
function broadcastKeywordsToContentScripts() {
  browser.tabs.query({}, (tabs) => {
    tabs.forEach((tab) => {
      browser.tabs.sendMessage(tab.id, {
        type: 'KEYWORDS_UPDATE',
        contentKeywords: contentKeywords
      }).catch(() => {
        // Ignore errors for tabs that don't have content scripts
      });
    });
  });
}

// Set up SSE connection for real-time keyword updates
function setupSSEConnection() {
  console.log('Setting up centralized SSE connection for keyword updates...');
  
  // Clean up existing connection if any
  if (window.backgroundSSE) {
    window.backgroundSSE.close();
    window.backgroundSSE = null;
  }
  
  const eventSource = new EventSource('http://127.0.0.1/keywords-stream');
  
  eventSource.onopen = function(event) {
    console.log('SSE connection opened');
    glockerConnected = true;
    updateStatusIcon();
  };
  
  eventSource.onmessage = function(event) {
    // Skip processing if cleaned up
    if (backgroundCleanedUp) return;
    
    console.log('SSE message received:', event.data);
    try {
      const data = JSON.parse(event.data);
      let updated = false;
      
      if (data.url_keywords && Array.isArray(data.url_keywords)) {
        urlKeywords = data.url_keywords;
        console.log('Updated URL keywords via SSE:', urlKeywords);
        updated = true;
      }
      
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated content keywords via SSE:', contentKeywords);
        updated = true;
      }
      
      // Recompile regex patterns and broadcast updates
      if (updated) {
        compileKeywordRegexes();
        broadcastKeywordsToContentScripts();
      }
    } catch (error) {
      console.log('Failed to parse SSE message:', error);
    }
  };
  
  eventSource.onerror = function(event) {
    console.log('SSE connection error:', event);
    glockerConnected = false;
    updateStatusIcon();
    // Connection will automatically retry unless cleaned up
    if (backgroundCleanedUp) {
      eventSource.close();
    }
  };
  
  // Store reference for cleanup
  window.backgroundSSE = eventSource;
}

// Handle messages from content scripts
browser.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'GET_KEYWORDS') {
    // Send current keywords to requesting content script
    sendResponse({
      contentKeywords: contentKeywords
    });
  }
});

// Background script cleanup function
function cleanupBackground() {
  if (backgroundCleanedUp) return;
  
  console.log('Cleaning up background script resources');
  backgroundCleanedUp = true;
  
  // Close SSE connection
  if (window.backgroundSSE) {
    window.backgroundSSE.close();
    window.backgroundSSE = null;
  }
  
  // Clear keyword arrays
  urlKeywords = null;
  contentKeywords = null;
  
  console.log('Background cleanup completed');
}

// Set up cleanup for background script
browser.runtime.onSuspend.addListener(cleanupBackground);

// Initialize status icon and keywords on startup
updateStatusIcon(); // Set initial disconnected state

// Compile initial regex patterns
compileKeywordRegexes();

fetchKeywords().then(() => {
  // Set up centralized SSE connection for real-time updates
  setupSSEConnection();
}).catch(() => {
  // Still set up SSE connection even if initial fetch failed
  setupSSEConnection();
});

browser.webRequest.onBeforeRequest.addListener(
  function(details) {
    const url = details.url.toLowerCase();
    
    // Skip checking localhost/127.0.0.1 URLs to prevent redirect loops
    if (url.includes('127.0.0.1') || url.includes('localhost')) {
      return;
    }
    
    for (let keywordData of urlKeywordRegexes) {
      if (keywordData.regex.test(url)) {
        console.log("Found ", keywordData.keyword, " in ", url);
        // Report to glocker
        fetch('http://127.0.0.1/report', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            url: details.url,
            trigger: `url-keyword:${keywordData.keyword}`,
            timestamp: Date.now()
          })
        }).catch(() => {}); // Ignore failures
        
        // Redirect to blocked page with reason
        const reason = encodeURIComponent(`URL contains blocked keyword: "${keywordData.keyword}"`);
        return {redirectUrl: `http://127.0.0.1/blocked?reason=${reason}`};
      }
    }
  },
  {urls: ["<all_urls>"]},
  ["blocking"]
);

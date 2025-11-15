// Page content analysis - will be populated from server
let contentKeywords = ['trigger1', 'trigger2']; // fallback defaults

// Fetch keywords from glocker server
async function fetchKeywords() {
  console.log('Fetching keywords from server...');
  try {
    const response = await fetch('http://127.0.0.1/keywords');
    console.log('Keywords fetch response status:', response.status);
    
    if (response.ok) {
      const data = await response.json();
      console.log('Keywords response data:', data);
      if (data.content_keywords && Array.isArray(data.content_keywords)) {
        contentKeywords = data.content_keywords;
        console.log('Updated Content keywords from server:', contentKeywords);
      } else {
        console.log('No valid content_keywords in response, using defaults');
      }
      return data;
    } else {
      console.log('Keywords fetch failed with status:', response.status);
    }
  } catch (error) {
    console.log('Keywords fetch error:', error);
  }
  return null;
}

function analyzeContent() {
  console.log('analyzeContent() called for URL:', window.location.href);
  
  // Skip analyzing localhost/127.0.0.1 pages to prevent redirect loops
  if (window.location.hostname === '127.0.0.1' || window.location.hostname === 'localhost') {
    console.log('Skipping localhost analysis to prevent redirect loops');
    return;
  }
  
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  console.log('Analyzing content, text length:', text.length);
  console.log('Current keywords to check:', contentKeywords);
  
  for (let keyword of contentKeywords) {
    console.log('Checking for keyword:', keyword);
    // Use word boundaries to match whole words only
    const regex = new RegExp('\\b' + keyword.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i');
    if (regex.test(text)) {
      console.log('KEYWORD MATCH FOUND:', keyword);
      const reportData = {
        url: window.location.href,
        domain: window.location.hostname,
        trigger: `content-keyword:${keyword}`,
        timestamp: Date.now()
      };
      
      console.log('Sending report:', reportData);
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(reportData)
      }).catch((error) => {
        console.log('Report send failed:', error);
      });
      
      // Redirect to blocked page with reason
      const reason = encodeURIComponent(`Page content contains blocked keyword: "${keyword}"`);
      console.log('Redirecting to blocked page with reason:', reason);
      window.location.replace(`http://127.0.0.1/blocked?reason=${reason}`);
      
      break; // Only report once per page
    }
  }
  console.log('Content analysis complete, no matches found');
}

// Set up MutationObserver to watch for dynamically loaded content
function setupContentMonitoring() {
  console.log('Setting up content monitoring...');
  
  // Initial content analysis
  analyzeContent();
  
  // Watch for new content being added to the page
  const observer = new MutationObserver((mutations) => {
    console.log('MutationObserver triggered, mutations count:', mutations.length);
    let shouldAnalyze = false;
    
    mutations.forEach((mutation) => {
      // Check if new nodes were added that might contain text
      if (mutation.type === 'childList' && mutation.addedNodes.length > 0) {
        console.log('Child nodes added:', mutation.addedNodes.length);
        for (let node of mutation.addedNodes) {
          // Only analyze if text content was actually added
          if (node.nodeType === Node.TEXT_NODE || 
              (node.nodeType === Node.ELEMENT_NODE && node.textContent.trim())) {
            console.log('Text content detected in new node, will analyze');
            shouldAnalyze = true;
            break;
          }
        }
      }
    });
    
    if (shouldAnalyze) {
      console.log('Scheduling content analysis due to DOM changes');
      // Debounce rapid changes - wait a bit before analyzing
      clearTimeout(window.glocketContentAnalysisTimeout);
      window.glocketContentAnalysisTimeout = setTimeout(analyzeContent, 500);
    }
  });
  
  // Start observing changes to the entire document
  const targetNode = document.body || document.documentElement;
  console.log('Starting MutationObserver on:', targetNode.tagName);
  observer.observe(targetNode, {
    childList: true,
    subtree: true,
    characterData: true
  });
  console.log('Content monitoring setup complete');
}

console.log("Content script starting on:", window.location.href);
console.log("Document ready state:", document.readyState);

// Initialize keywords on startup
fetchKeywords().then((data) => {
  console.log('Keywords fetch completed, data:', data);
  // Run content analysis after keywords are loaded
  if (document.readyState === 'loading') {
    console.log('Document still loading, waiting for DOMContentLoaded');
    document.addEventListener('DOMContentLoaded', setupContentMonitoring);
  } else {
    console.log('Document already loaded, setting up monitoring immediately');
    setupContentMonitoring();
  }
}).catch((error) => {
  console.log('Keywords fetch failed, using defaults:', error);
  // Still try to analyze with default keywords
  setupContentMonitoring();
});

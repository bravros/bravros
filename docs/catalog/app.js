// State
let skillsData = [];
let activeCategory = 'all';
let searchQuery = '';
let activeLang = 'en';
let activeSection = 'skills';

// Icons SVG Map
const iconMap = {
    'git-commit': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="4"></circle><line x1="1.05" y1="12" x2="7" y2="12"></line><line x1="17" y1="12" x2="22.95" y2="12"></line></svg>',
    'search': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>',
    'workflow': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="9" rx="1"></rect><rect x="14" y="3" width="7" height="5" rx="1"></rect><rect x="14" y="12" width="7" height="9" rx="1"></rect><rect x="3" y="16" width="7" height="5" rx="1"></rect></svg>',
    'network': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="16" y="16" width="6" height="6" rx="1"></rect><rect x="2" y="16" width="6" height="6" rx="1"></rect><rect x="9" y="2" width="6" height="6" rx="1"></rect><path d="M12 8v8M12 8h-9v8M12 8h9v8"></path></svg>',
    'refresh': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"></svg>',
    'shield': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path></svg>',
    'code': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"></polyline><polyline points="8 6 2 12 8 18"></polyline></svg>',
    'settings': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>',
    'terminal': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 17 10 11 4 5"></polyline><line x1="12" y1="19" x2="20" y2="19"></line></svg>',
    'git-branch': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="6" y1="3" x2="6" y2="15"></line><circle cx="18" cy="6" r="3"></circle><circle cx="6" cy="18" r="3"></circle><path d="M18 9a9 9 0 0 1-9 9"></path></svg>'
};

// Elements
const skillsGrid = document.getElementById('skills-grid');
const searchInput = document.getElementById('search-input');
const categoryFilters = document.getElementById('category-filters');
const langSelector = document.querySelector('.lang-selector');

const navBtnSkills = document.getElementById('nav-btn-skills');
const navBtnSecurity = document.getElementById('nav-btn-security');
const sectionSkills = document.getElementById('section-skills');
const sectionSecurity = document.getElementById('section-security');

const btnCopyInstall = document.getElementById('btn-copy-install');
const btnCopyKey = document.getElementById('btn-copy-key');
const installCmd = document.getElementById('install-cmd');
const minisignKey = document.getElementById('minisign-key');

// Drawer Elements
const skillDrawer = document.getElementById('skill-drawer');
const drawerOverlay = document.getElementById('drawer-overlay');
const btnCloseDrawer = document.getElementById('btn-close-drawer');
const drawerTitle = document.getElementById('drawer-title');
const drawerCategory = document.getElementById('drawer-category');
const drawerDescription = document.getElementById('drawer-description');
const drawerCode = document.getElementById('drawer-code');
const drawerIconContainer = document.querySelector('.drawer-icon-container');

// Fetch and Init
async function init() {
    try {
        const res = await fetch('catalog.json');
        skillsData = await res.json();
        renderSkills();
    } catch (err) {
        console.error('Failed to fetch skill catalog:', err);
    }
}

// Render functions
function renderSkills() {
    skillsGrid.innerHTML = '';
    
    const filtered = skillsData.filter(skill => {
        // Category check
        if (activeCategory !== 'all' && skill.category !== activeCategory) {
            return false;
        }
        
        // Search query check
        if (searchQuery) {
            const meta = skill.metadata[activeLang] || skill.metadata['en'];
            const name = meta.name.toLowerCase();
            const desc = meta.description.toLowerCase();
            const slug = skill.slug.toLowerCase();
            const body = skill.body.toLowerCase();
            
            return name.includes(searchQuery) || 
                   desc.includes(searchQuery) || 
                   slug.includes(searchQuery) ||
                   body.includes(searchQuery);
        }
        
        return true;
    });

    if (filtered.length === 0) {
        skillsGrid.innerHTML = `<div class="no-results">No skills found matching "${searchQuery}".</div>`;
        return;
    }

    filtered.forEach(skill => {
        const meta = skill.metadata[activeLang] || skill.metadata['en'];
        const card = document.createElement('div');
        card.className = 'skill-card';
        card.id = `skill-${skill.slug}`;
        
        const svgIcon = iconMap[skill.icon] || iconMap['terminal'];
        
        card.innerHTML = `
            <div class="card-header">
                <div class="icon-container">
                    ${svgIcon}
                </div>
                <span class="category-badge ${skill.category}">${skill.category}</span>
            </div>
            <div class="card-body">
                <h3>${meta.name}</h3>
                <p>${meta.description}</p>
            </div>
        `;
        
        card.addEventListener('click', () => openDrawer(skill));
        skillsGrid.appendChild(card);
    });
}

function openDrawer(skill) {
    const meta = skill.metadata[activeLang] || skill.metadata['en'];
    drawerTitle.innerText = meta.name;
    drawerCategory.innerText = skill.category;
    drawerCategory.className = `drawer-category-badge ${skill.category}`;
    drawerDescription.innerText = meta.description;
    drawerCode.innerText = skill.body;
    
    const svgIcon = iconMap[skill.icon] || iconMap['terminal'];
    drawerIconContainer.innerHTML = svgIcon;
    
    skillDrawer.classList.add('active');
    drawerOverlay.classList.add('active');
}

function closeDrawer() {
    skillDrawer.classList.remove('active');
    drawerOverlay.classList.remove('active');
}

// Navigation switcher
function switchSection(section) {
    activeSection = section;
    if (section === 'skills') {
        navBtnSkills.classList.add('active');
        navBtnSecurity.classList.remove('active');
        sectionSkills.classList.add('active');
        sectionSecurity.classList.remove('active');
    } else {
        navBtnSkills.classList.remove('active');
        navBtnSecurity.classList.add('active');
        sectionSkills.classList.remove('active');
        sectionSecurity.classList.add('active');
    }
}

// Clipboard copying with visual feedback
function copyToClipboard(text, button) {
    navigator.clipboard.writeText(text).then(() => {
        const orig = button.innerText;
        button.innerText = 'Copied!';
        button.style.backgroundColor = '#10b981';
        setTimeout(() => {
            button.innerText = orig;
            button.style.backgroundColor = '';
        }, 2000);
    });
}

// Event Listeners
searchInput.addEventListener('input', (e) => {
    searchQuery = e.target.value.toLowerCase().trim();
    renderSkills();
});

categoryFilters.addEventListener('click', (e) => {
    if (e.target.classList.contains('filter-btn')) {
        document.querySelectorAll('.filter-btn').forEach(btn => btn.classList.remove('active'));
        e.target.classList.add('active');
        activeCategory = e.target.getAttribute('data-category');
        renderSkills();
    }
});

langSelector.addEventListener('click', (e) => {
    if (e.target.classList.contains('lang-btn')) {
        document.querySelectorAll('.lang-btn').forEach(btn => btn.classList.remove('active'));
        e.target.classList.add('active');
        activeLang = e.target.getAttribute('data-lang');
        renderSkills();
    }
});

navBtnSkills.addEventListener('click', () => switchSection('skills'));
navBtnSecurity.addEventListener('click', () => switchSection('security'));

btnCopyInstall.addEventListener('click', () => copyToClipboard(installCmd.innerText, btnCopyInstall));
btnCopyKey.addEventListener('click', () => copyToClipboard(minisignKey.innerText, btnCopyKey));

btnCloseDrawer.addEventListener('click', closeDrawer);
drawerOverlay.addEventListener('click', closeDrawer);

// Run init
init();

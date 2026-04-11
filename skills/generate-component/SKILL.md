**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

**Language override:** Generated UI code uses PT-BR text by default. Your responses/explanations follow the user's language.

# Livewire + DaisyUI Component Factory

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

Generate production-ready Livewire 4 components with Blade views following the TALL stack conventions used across all projects.

## Usage

Run `/generate-component` in any TALL stack project and describe what you need in natural language.

**Examples:**
- "datatable for orders with filters by status, date range, carrier. Sortable by date and amount. Export button."
- "form to create a new product with name, price (BRL cents), description, image upload"
- "stats dashboard with 4 KPI cards and a line chart for monthly revenue"
- "detail page for a customer showing their info and order history"

## How It Works

1. User describes the component in natural language
2. Claude asks clarifying questions (model name, permissions, fields)
3. Generates all files:
   - `app/Livewire/{Namespace}/{Name}.php` — Livewire component class
   - `resources/views/livewire/{namespace}/{name}.blade.php` — Blade view
   - `app/Livewire/Concerns/{Name}Trait.php` — extracted traits when applicable
   - `tests/Feature/Livewire/{Namespace}/{Name}Test.php` — Pest tests

## Component Patterns (from existing codebase)

### Index/List Page Pattern
Based on `Pedidos/IndexV2` and `Consumidores/Index`:

```php
<?php

namespace App\Livewire\{Namespace};

use Livewire\Attributes\Computed;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Title;
use Livewire\Attributes\Url;
use Livewire\Component;
use Livewire\WithPagination;

#[Layout('components.layouts.app')]
#[Title('{Page Title}')]
class Index extends Component
{
    use WithPagination;

    #[Url(except: '')]
    public string $search = '';

    #[Url(except: 10)]
    public int $perPage = 10;

    #[Url(except: 'id')]
    public string $sortField = 'id';

    #[Url(except: 'desc')]
    public string $sortDirection = 'desc';

    public function mount(): void
    {
        // Permission check
        if (! auth()->user()->can('{permission_name}')) {
            abort(403);
        }
    }

    public function updatingSearch(): void
    {
        $this->resetPage();
    }

    public function sortBy(string $field): void
    {
        if ($this->sortField === $field) {
            $this->sortDirection = $this->sortDirection === 'asc' ? 'desc' : 'asc';
        } else {
            $this->sortField = $field;
            $this->sortDirection = 'asc';
        }
    }

    #[Computed]
    public function items()
    {
        return Model::query()
            ->when($this->search, fn ($q) => $q->where('name', 'like', "%{$this->search}%"))
            ->orderBy($this->sortField, $this->sortDirection)
            ->paginate($this->perPage);
    }

    public function render()
    {
        return view('livewire.{namespace}.index');
    }
}
```

### Blade View Conventions (DaisyUI v5 + Tailwind v4)

```blade
<div>
    {{-- Page header --}}
    <x-page-header title="{Title}">
        <x-slot name="actions">
            <a href="{{ route('{resource}.create') }}" class="btn btn-primary btn-sm">
                Novo {Resource}
            </a>
        </x-slot>
    </x-page-header>

    {{-- Filters bar --}}
    <div class="card bg-base-200 mb-4">
        <div class="card-body p-4">
            <div class="flex flex-wrap items-center gap-3">
                {{-- Search --}}
                <label class="input input-bordered input-sm flex items-center gap-2 grow max-w-xs">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 opacity-70" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
                    <input type="text" wire:model.live.debounce.300ms="search" placeholder="Buscar..." class="grow" />
                </label>

                {{-- Per page --}}
                <select wire:model.live="perPage" class="select select-bordered select-sm w-auto">
                    <option value="10">10</option>
                    <option value="25">25</option>
                    <option value="50">50</option>
                </select>
            </div>
        </div>
    </div>

    {{-- Data table --}}
    <div class="card bg-base-100">
        <div class="overflow-x-auto">
            <table class="table table-zebra">
                <thead>
                    <tr>
                        <th wire:click="sortBy('id')" class="cursor-pointer">
                            # @if($sortField === 'id') {{ $sortDirection === 'asc' ? '▲' : '▼' }} @endif
                        </th>
                        {{-- More columns --}}
                    </tr>
                </thead>
                <tbody>
                    @forelse($this->items as $item)
                        <tr>
                            <td>{{ $item->id }}</td>
                            {{-- More cells --}}
                        </tr>
                    @empty
                        <tr>
                            <td colspan="99" class="text-center py-8 text-base-content/50">
                                Nenhum registro encontrado.
                            </td>
                        </tr>
                    @endforelse
                </tbody>
            </table>
        </div>
        <div class="card-body pt-2">
            {{ $this->items->links() }}
        </div>
    </div>
</div>
```

## Rules & Conventions

### PHP / Livewire
- **Livewire 4** with PHP 8.3+ typed properties
- Use `#[Layout('components.layouts.app')]` attribute (not `->layout()`)
- Use `#[Title('...')]` attribute
- Use `#[Url]` for query string params
- Use `#[Computed]` for derived data
- Permission checks in `mount()` using `auth()->user()->can()`
- Money stored as **integer cents** — display with `number_format($value / 100, 2, ',', '.')` for BRL
- Extract reusable logic into `app/Livewire/Concerns/` traits
- Use service classes for complex queries (e.g., `PedidosQueryService`)

### Blade / UI
- **DaisyUI v5** component classes: `btn`, `card`, `table`, `input`, `select`, `badge`, `alert`, `modal`, `dropdown`, `tabs`
- **Dark-first** theme — use `bg-base-100`, `bg-base-200`, `text-base-content`, etc.
- Status badges: `badge badge-success`, `badge badge-warning`, `badge badge-error`, `badge badge-info`
- Buttons: `btn btn-primary btn-sm`, `btn btn-ghost btn-sm`, `btn btn-outline`
- Cards: `card bg-base-100` with `card-body`
- Tables: `table table-zebra` inside `overflow-x-auto`
- All UI text in **Portuguese (pt_BR)** by default
- Use `wire:model.live.debounce.300ms` for search inputs
- Use `wire:click` for actions, `wire:confirm` for destructive ones

### Testing (Pest)
- Feature test for every component
- Test permission check (403)
- Test rendering
- Test search/filter functionality
- Test sorting
- Test pagination
- Use model factories
- Run targeted: `vendor/bin/pest --filter="ComponentNameTest"`

### File Naming
- Component: `app/Livewire/{Namespace}/{PascalCase}.php`
- View: `resources/views/livewire/{kebab-namespace}/{kebab-name}.blade.php`
- Test: `tests/Feature/Livewire/{Namespace}/{PascalCase}Test.php`
- Trait: `app/Livewire/Concerns/{PascalCase}.php`

## Advanced Patterns

### Status Tabs (like Pedidos)
When the component needs status-based tab filtering, include:
- `$activeTab` with `#[Url(as: 'tab', except: 'all')]`
- `setStatusTab()` method
- Status counts as `#[Computed]`
- DaisyUI tab buttons with counts as badges

### Bulk Selection
When the component needs multi-select + bulk actions:
- `$selected` array and `$selectAll` boolean
- `updatedSelectAll()` / `updatedSelected()` methods
- `clearSelection()` method
- Checkbox column in table
- Floating action bar when items selected

### Date Range Presets
Quick date filters: today, week, month, quarter
- `setDatePreset(string $preset)` method
- DaisyUI button group for presets

### Export
- `exportSelected()` redirects to export route
- Export button disabled when nothing selected

## Process

When the user describes a component:

## Step 1/4: Detect Stack and Clarify Requirements

```bash
echo "🎨 generate-component [1/4] Detecting stack versions and clarifying requirements"
```

**Detect stack versions**:
```bash
LIVEWIRE_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.livewire 2>/dev/null)
DAISYUI_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.daisyui 2>/dev/null)
PROJECT_TYPE=$(~/.claude/bin/bravros detect-stack --versions --field project_type 2>/dev/null)
```
- Use the detected Livewire and DaisyUI versions to select the correct APIs and component classes.
- If `PROJECT_TYPE` is `"api"`, warn the user: "This project appears to be an API — Livewire/Blade component generation may not be needed. Confirm to proceed."
- **Identify the pattern**: Is it a list/index, form/create, detail/show, or dashboard?
- **Ask what's missing**: Model name, permission name, specific fields, relationships

## Step 2/4: Generate Component Files

```bash
echo "🎨 generate-component [2/4] Generating Livewire component and Blade view"
```

Generate all files in the correct project directories.

## Step 3/4: Generate Tests

```bash
echo "🎨 generate-component [3/4] Generating Pest feature tests"
```

Generate Pest tests covering the main flows (permission check, rendering, search/filter, sorting, pagination).

## Step 4/4: Verify and Report

```bash
echo "🎨 generate-component [4/4] Reporting results and next steps"
```

- Show the route to add to `routes/web.php`
- Suggest running `vendor/bin/pest --filter="ComponentTest"` to verify

$ErrorActionPreference = "Stop"

$repo = "wakadorimk2/enterlight"
$tag = "v0.1.0"

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  throw "GitHub CLI (gh) is required: https://cli.github.com/"
}

gh auth status

if (-not (Test-Path ".git")) {
  git init -b main
  git add .
  git commit -m "Release Enterlight v0.1.0"
}

$exists = $false
try {
  gh repo view $repo | Out-Null
  $exists = $true
} catch {}

if (-not $exists) {
  gh repo create $repo --public --source . --remote origin --push `
    --description "Use a Razer keyboard's Enter key as a status beacon for coding agents."
} else {
  if (-not (git remote get-url origin 2>$null)) {
    git remote add origin "https://github.com/$repo.git"
  }
  git push -u origin main
}

if (-not (git tag -l $tag)) {
  git tag -a $tag -m "Enterlight $tag"
}
git push origin $tag

gh release create $tag `
  dist/enterlight-windows-amd64.exe `
  dist/SHA256SUMS.txt `
  --title "Enterlight $tag" `
  --notes-file RELEASE_NOTES.md

Write-Host "Published https://github.com/$repo/releases/tag/$tag"

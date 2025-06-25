#!/bin/bash
# Simple test script to verify the validation script works locally

echo "Testing Quick Start validation script locally..."
echo "This will start the dev environment, run the validation, and clean up."
echo

# Check if binaries exist
if [ ! -f "./bin/pcas" ] || [ ! -f "./bin/pcasctl" ]; then
    echo "Building binaries first..."
    make build
fi

# Run the validation script
./scripts/validate_quickstart.sh

exit_code=$?

if [ $exit_code -eq 0 ]; then
    echo
    echo "✅ Local test passed!"
else
    echo
    echo "❌ Local test failed with exit code: $exit_code"
fi

exit $exit_code
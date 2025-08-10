#!/bin/bash

# Test that all examples compile successfully
# This doesn't run them, just verifies they build without errors

echo "🧪 Testing Example Compilation"
echo "=============================="
echo

SUCCESS_COUNT=0
TOTAL_COUNT=0

test_example() {
    local name="$1"
    local dir="$2"
    
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    echo -n "📦 Testing $name... "
    
    cd "$dir"
    
    # Install dependencies silently
    if go mod tidy &>/dev/null; then
        # Try to build (not run) the example
        if go build -o test_binary main.go &>/dev/null; then
            echo "✅ PASS"
            rm -f test_binary  # Clean up
            SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        else
            echo "❌ FAIL (build error)"
        fi
    else
        echo "❌ FAIL (dependency error)"
    fi
    
    cd ..
}

# Test all examples
test_example "Web Server" "web-server"
test_example "Microservice" "microservice" 
test_example "CLI Tool" "cli-tool"
test_example "Worker Pool" "worker-pool"
test_example "Monitoring Service" "monitoring"

echo
echo "📊 Results: $SUCCESS_COUNT/$TOTAL_COUNT examples compiled successfully"
echo

if [ $SUCCESS_COUNT -eq $TOTAL_COUNT ]; then
    echo "🎉 All examples are ready to run!"
    echo
    echo "Next steps:"
    echo "  1. Run ./quick_start.sh for interactive testing"
    echo "  2. Or manually: cd <example-dir> && go run main.go"
    echo "  3. Read RUN_EXAMPLES.md for detailed instructions"
else
    echo "⚠️  Some examples had issues. Check the errors above."
    exit 1
fi
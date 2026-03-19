// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {FaucetToken} from "../src/FaucetToken.sol";

contract FaucetTokenTest is Test {
    FaucetToken public token;

    address public alice;
    address public bob;

    function setUp() public {
        token = new FaucetToken();
        alice = makeAddr("alice");
        bob = makeAddr("bob");
    }

    // ──────────────────────── metadata tests ──────────────────────────

    function test_name() public view {
        assertEq(token.name(), "Bastion Faucet Token");
    }

    function test_symbol() public view {
        assertEq(token.symbol(), "BFT");
    }

    function test_decimals() public view {
        assertEq(token.decimals(), 18);
    }

    function test_claimAmount() public view {
        assertEq(token.CLAIM_AMOUNT(), 100 ether);
    }

    function test_claimCooldown() public view {
        assertEq(token.CLAIM_COOLDOWN(), 1 hours);
    }

    // ──────────────────────── first claim tests ───────────────────────

    function test_claim_firstClaimSucceeds() public {
        vm.prank(alice);
        token.claim();
        assertEq(token.balanceOf(alice), 100 ether);
    }

    function test_claim_firstClaimSucceedsAtTimestampOne() public {
        // Anvil starts at timestamp 1 — this must not revert.
        vm.warp(1);
        vm.prank(alice);
        token.claim();
        assertEq(token.balanceOf(alice), 100 ether);
    }

    function test_claim_setsLastClaimed() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();
        assertEq(token.lastClaimed(alice), 1000);
    }

    function test_claim_mintsCorrectAmount() public {
        vm.prank(alice);
        token.claim();
        assertEq(token.totalSupply(), 100 ether);
        assertEq(token.balanceOf(alice), 100 ether);
    }

    // ──────────────────────── cooldown tests ──────────────────────────

    function test_claim_revertsBeforeCooldown() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        // Try again immediately — should revert.
        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(FaucetToken.ClaimCooldown.selector, 1000 + 1 hours));
        token.claim();
    }

    function test_claim_revertsOneSecondBeforeCooldown() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        vm.warp(1000 + 1 hours - 1);
        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(FaucetToken.ClaimCooldown.selector, 1000 + 1 hours));
        token.claim();
    }

    function test_claim_succeedsExactlyAtCooldown() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        vm.warp(1000 + 1 hours);
        vm.prank(alice);
        token.claim();
        assertEq(token.balanceOf(alice), 200 ether);
    }

    function test_claim_succeedsAfterCooldown() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        vm.warp(1000 + 2 hours);
        vm.prank(alice);
        token.claim();
        assertEq(token.balanceOf(alice), 200 ether);
    }

    function test_claim_updatesLastClaimedOnSecondClaim() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        vm.warp(1000 + 2 hours);
        vm.prank(alice);
        token.claim();
        assertEq(token.lastClaimed(alice), 1000 + 2 hours);
    }

    // ──────────────────── independent accounts tests ──────────────────

    function test_claim_independentPerAccount() public {
        vm.prank(alice);
        token.claim();

        // Bob can claim immediately — not blocked by Alice's cooldown.
        vm.prank(bob);
        token.claim();

        assertEq(token.balanceOf(alice), 100 ether);
        assertEq(token.balanceOf(bob), 100 ether);
        assertEq(token.totalSupply(), 200 ether);
    }

    function test_claim_cooldownIsPerAccount() public {
        vm.warp(1000);
        vm.prank(alice);
        token.claim();

        // Alice is on cooldown, but Bob is not.
        vm.warp(1000 + 30 minutes);

        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(FaucetToken.ClaimCooldown.selector, 1000 + 1 hours));
        token.claim();

        vm.prank(bob);
        token.claim();
        assertEq(token.balanceOf(bob), 100 ether);
    }

    // ──────────────────────── error payload tests ─────────────────────

    function test_claim_errorPayloadContainsCorrectTimestamp() public {
        vm.warp(5000);
        vm.prank(alice);
        token.claim();

        vm.warp(5000 + 1800);
        vm.prank(alice);
        vm.expectRevert(abi.encodeWithSelector(FaucetToken.ClaimCooldown.selector, 5000 + 1 hours));
        token.claim();
    }
}
